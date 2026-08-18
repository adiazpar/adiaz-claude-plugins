package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// CompiledBuildID is set by release packaging through -ldflags -X. Development
// builds retain an explicit identity instead of pretending to be a release.
var CompiledBuildID = "development"

var (
	executableIdentityOnce sync.Once
	executableIdentity     string
)

type IndexManager struct {
	Boundary  Boundary
	CacheRoot string
	Settings  KnowledgeSettings
	Manifest  ModelManifest
}

func DefaultCacheRoot(boundary Boundary) string {
	return filepath.Join(boundary.Root, ".re-discipline", "cache", "knowledge")
}

func ProbeRuntimeIdentity(manifest ModelManifest) (RuntimeIdentity, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer db.Close()
	var version string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		return RuntimeIdentity{}, err
	}
	rows, err := db.Query("PRAGMA compile_options")
	if err != nil {
		return RuntimeIdentity{}, err
	}
	options := []string{}
	for rows.Next() {
		var option string
		if err := rows.Scan(&option); err != nil {
			rows.Close()
			return RuntimeIdentity{}, err
		}
		options = append(options, option)
	}
	rows.Close()
	sort.Strings(options)
	build := "sha256:" + SHA256String(strings.Join(options, "\n"))
	return RuntimeIdentity{
		Implementation:   manifest.Runtime.Implementation,
		Version:          RuntimeVersion,
		GoVersion:        runtime.Version(),
		CompiledBuildID:  CompiledBuildID,
		ExecutableSHA256: currentExecutableSHA256(),
		SQLiteDriver:     manifest.Runtime.SQLiteDriver,
		SQLiteVersion:    version,
		SQLiteBuild:      build,
		NumericalBackend: manifest.Runtime.NumericalBackend,
		TieBreaker:       manifest.Runtime.TieBreaker,
	}, nil
}

func currentExecutableSHA256() string {
	executableIdentityOnce.Do(func() {
		executableIdentity = "unavailable"
		path, err := os.Executable()
		if err != nil {
			return
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return
		}
		executableIdentity = "sha256:" + SHA256Bytes(body)
	})
	return executableIdentity
}

func (manager IndexManager) Inventory() (SourceInventory, error) {
	return DiscoverSources(manager.Boundary, manager.Settings)
}

func (manager IndexManager) Ensure(ctx context.Context) (Generation, SourceInventory, bool, error) {
	return manager.ensure(ctx, false)
}

func (manager IndexManager) Reconcile(ctx context.Context) (Generation, SourceInventory, bool, error) {
	return manager.ensure(ctx, true)
}

func (manager IndexManager) ensure(
	ctx context.Context,
	verifyContent bool,
) (Generation, SourceInventory, bool, error) {
	runtimeIdentity, err := ProbeRuntimeIdentity(manager.Manifest)
	if err != nil {
		return Generation{}, SourceInventory{}, false, err
	}
	current, err := manager.LoadCurrent()
	modelFingerprint := modelIndexFingerprint(manager.Manifest)
	settingsFingerprint := IndexingSettingsDigest(manager.Settings)
	currentGitRevision, currentDirtyFingerprint := gitState(manager.Boundary.Root)
	corruptPath := ""
	if err != nil {
		corruptPath = manager.corruptCurrentDatabase()
	}
	// The settings fingerprint is checked on the fast path specifically. Every
	// other freshness signal here is derived from a file the corpus contains,
	// and a source-class toggle changes no such file - so without this the fast
	// path proves the index matches the documents it already knows about and
	// says nothing about the documents it was told to start indexing.
	if err == nil && current.ModelFingerprint == modelFingerprint &&
		current.SettingsFingerprint == settingsFingerprint &&
		current.Runtime == runtimeIdentity && current.ParserVersion == ParserVersion &&
		current.ChunkerVersion == ChunkerVersion &&
		current.GitRevision == currentGitRevision &&
		!verifyContent && manager.FastFresh(current) {
		if err := verifyDatabase(current.Database); err == nil {
			current.GitRevision = currentGitRevision
			current.DirtyFingerprint = currentDirtyFingerprint
			return current, SourceInventory{}, false, nil
		} else {
			corruptPath = current.Database
		}
	}
	inventory, err := manager.Inventory()
	if err != nil {
		return Generation{}, inventory, false, err
	}

	if err := os.MkdirAll(manager.CacheRoot, 0o700); err != nil {
		return Generation{}, inventory, false, err
	}
	resolvedCache, err := canonicalExistingPath(manager.CacheRoot)
	if err != nil {
		return Generation{}, inventory, false, fmt.Errorf(
			"resolve index cache root: %w", err)
	}
	manager.CacheRoot = resolvedCache
	generationsPath, err := containedOutputPath(
		manager.CacheRoot, "generations",
	)
	if err != nil {
		return Generation{}, inventory, false, err
	}
	if err := os.MkdirAll(generationsPath, 0o700); err != nil {
		return Generation{}, inventory, false, err
	}
	lockPath, err := containedOutputPath(manager.CacheRoot, "writer.lock")
	if err != nil {
		return Generation{}, inventory, false, err
	}
	lock, err := acquireWriterLock(lockPath)
	if err != nil {
		if errors.Is(err, errWriterLocked) && current.ID != "" &&
			current.ModelFingerprint == modelFingerprint &&
			current.Runtime == runtimeIdentity &&
			current.ParserVersion == ParserVersion &&
			current.ChunkerVersion == ChunkerVersion {
			if verifyErr := verifyDatabase(current.Database); verifyErr == nil {
				revisionStale := current.GitRevision != currentGitRevision
				current.GitRevision = currentGitRevision
				current.DirtyFingerprint = currentDirtyFingerprint
				current.ServingStale =
					current.CorpusFingerprint != inventory.Fingerprint ||
						current.SettingsFingerprint != settingsFingerprint ||
						revisionStale
				current.WriterContention = true
				inventory.Diagnostics = append(inventory.Diagnostics,
					"writer contention; serving the last verified complete generation")
				return current, inventory, false, nil
			}
		}
		return Generation{}, inventory, false, fmt.Errorf("acquire index writer: %w", err)
	}
	defer lock.Close()

	current, err = manager.LoadCurrent()
	if err != nil {
		if candidate := manager.corruptCurrentDatabase(); candidate != "" {
			corruptPath = candidate
		}
	}
	reusePath := ""
	// The settings fingerprint is compared here too, not only on the fast path.
	// A toggle that adds a class the project has no files for leaves the corpus
	// fingerprint identical, so without this the generation would be accepted
	// unchanged and every later call would re-walk the corpus to rediscover the
	// same nothing.
	if err == nil && current.CorpusFingerprint == inventory.Fingerprint &&
		current.SettingsFingerprint == settingsFingerprint &&
		current.ModelFingerprint == modelFingerprint &&
		current.Runtime == runtimeIdentity &&
		current.GitRevision == currentGitRevision {
		if err := verifyDatabase(current.Database); err == nil {
			current.GitRevision = currentGitRevision
			current.DirtyFingerprint = currentDirtyFingerprint
			return current, inventory, false, nil
		} else {
			corruptPath = current.Database
		}
	}
	if err == nil && current.ModelFingerprint == modelFingerprint &&
		current.Runtime == runtimeIdentity && current.ParserVersion == ParserVersion &&
		current.ChunkerVersion == ChunkerVersion &&
		verifyDatabase(current.Database) == nil {
		reusePath = current.Database
	}
	// The document-level difference between the outgoing and the incoming corpus
	// exists only during a publication and was previously discarded. A session
	// resuming days later can ask what moved only if somebody wrote it down, so
	// read the outgoing document set now, while the generation it belongs to is
	// still current and still on disk.
	previousID := ""
	var previousDocuments map[string]documentRecord
	if err == nil && current.ID != "" {
		if documents, documentsErr := readGenerationDocuments(
			current.Database,
		); documentsErr == nil {
			previousID, previousDocuments = current.ID, documents
		}
	}
	generation, err := manager.build(ctx, inventory, runtimeIdentity, reusePath)
	if err != nil {
		return Generation{}, inventory, false, err
	}
	recordGenerationDelta(manager.CacheRoot, buildGenerationDelta(
		previousID, previousDocuments, generation, inventory))
	if corruptPath != "" && filepath.Clean(corruptPath) != filepath.Clean(generation.Database) {
		if err := quarantineCorruptGeneration(corruptPath); err != nil {
			return Generation{}, inventory, false, err
		}
	}
	manager.cleanupGenerations(generation.Database)
	return generation, inventory, true, nil
}

func quarantineCorruptGeneration(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	quarantine := path + ".corrupt-" +
		SHA256String(fmt.Sprintf("%s-%d", path, time.Now().UnixNano()))[:12]
	if err := replaceFile(path, quarantine); err != nil {
		return fmt.Errorf("quarantine corrupt generation: %w", err)
	}
	return nil
}

func (manager IndexManager) cleanupGenerations(activeDatabase string) {
	directory := filepath.Join(manager.CacheRoot, "generations")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type obsoleteCandidate struct {
		path       string
		generation string
		modTime    time.Time
		corrupt    bool
	}
	leased := leasedGenerationIDs(manager.CacheRoot)
	candidates := []obsoleteCandidate{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sqlite") && !strings.Contains(name, ".sqlite.corrupt-") {
			continue
		}
		path := filepath.Join(directory, name)
		if filepath.Clean(path) == filepath.Clean(activeDatabase) {
			continue
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, obsoleteCandidate{
			path: path, generation: strings.SplitN(name, ".sqlite", 2)[0],
			modTime: info.ModTime(),
			corrupt: strings.Contains(name, ".sqlite.corrupt-"),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	retainedComplete, retainedCorrupt := 0, 0
	for _, candidate := range candidates {
		if candidate.corrupt {
			if retainedCorrupt < 1 {
				retainedCorrupt++
				continue
			}
		} else if retainedComplete < 2 {
			retainedComplete++
			continue
		}
		if leased[candidate.generation] {
			// An active measurement run in this or another session is still
			// reading this generation. Retention reclaims it after a later
			// publication rather than failing that run.
			continue
		}
		// A Windows reader may still hold the immutable file. Cleanup is
		// intentionally best-effort and will retry after a future activation.
		_ = os.Remove(candidate.path)
	}
}

func (manager IndexManager) FastFresh(generation Generation) bool {
	if len(generation.SourceStates)+len(generation.DirectoryStates) == 0 {
		return false
	}
	for _, expected := range append(
		append([]SourceState(nil), generation.SourceStates...),
		generation.DirectoryStates...,
	) {
		actual, err := manager.currentState(expected.Path, expected.IsDir)
		if err != nil || actual != expected {
			return false
		}
	}
	return true
}

func (manager IndexManager) currentState(relative string, expectedDir bool) (SourceState, error) {
	if relative == "" || filepath.IsAbs(filepath.FromSlash(relative)) {
		return SourceState{}, errors.New("invalid generation source state path")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(relative, "\\", "/")))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return SourceState{}, errors.New("generation source state escapes project root")
	}
	absolute := filepath.Join(manager.Boundary.Root, clean)
	info, err := os.Stat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return SourceState{
				Path: filepath.ToSlash(clean), Exists: false, IsDir: expectedDir,
			}, nil
		}
		return SourceState{}, err
	}
	resolved, err := canonicalExistingPath(absolute)
	if err != nil || !withinRoot(manager.Boundary.Root, resolved) {
		return SourceState{}, errors.New("generation source state escapes project boundary")
	}
	return stateFromInfo(filepath.ToSlash(clean), info), nil
}

func (manager IndexManager) build(
	ctx context.Context,
	inventory SourceInventory,
	runtimeIdentity RuntimeIdentity,
	previousDatabase string,
) (Generation, error) {
	gitRevision, dirty := gitState(manager.Boundary.Root)
	project := filepath.Base(manager.Boundary.Root)
	for _, doc := range inventory.Documents {
		if doc.Path == ".re-discipline/project-profile.md" {
			for _, line := range strings.Split(doc.Content, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "name:") {
					project = strings.Trim(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), "\"'")
					break
				}
			}
		}
	}
	buildTime := time.Now().UTC()
	createdAt := RFC3339UTC(buildTime)
	settingsFingerprint := IndexingSettingsDigest(manager.Settings)
	idInput := struct {
		Corpus   string
		Settings string
		Models   string
		Runtime  RuntimeIdentity
		Nonce    string
	}{
		inventory.Fingerprint, settingsFingerprint,
		modelIndexFingerprint(manager.Manifest), runtimeIdentity,
		buildTime.Format(time.RFC3339Nano) + fmt.Sprintf("-%d", os.Getpid()),
	}
	digest, err := CanonicalDigest(idInput)
	if err != nil {
		return Generation{}, err
	}
	id := "generation-" + strings.TrimPrefix(digest, "sha256:")[:20]
	finalPath := filepath.Join(manager.CacheRoot, "generations", id+".sqlite")
	if err := verifyDatabase(finalPath); err == nil {
		generation, err := readGenerationMetadata(finalPath)
		if err == nil {
			if err := AtomicWriteJSON(filepath.Join(manager.CacheRoot, "current.json"), generation, 0o600); err != nil {
				return Generation{}, err
			}
			return generation, nil
		}
	}
	temp, err := os.CreateTemp(filepath.Join(manager.CacheRoot, "generations"), ".building-*.sqlite")
	if err != nil {
		return Generation{}, err
	}
	tempPath := temp.Name()
	temp.Close()
	_ = os.Remove(tempPath)
	defer os.Remove(tempPath)

	db, err := sql.Open("sqlite", tempPath)
	if err != nil {
		return Generation{}, err
	}
	defer db.Close()
	if err := createSchema(ctx, db); err != nil {
		return Generation{}, err
	}
	generation := Generation{
		ID: id, Database: finalPath, CorpusFingerprint: inventory.Fingerprint,
		SettingsFingerprint: settingsFingerprint,
		ModelFingerprint:    modelIndexFingerprint(manager.Manifest),
		Project:             project, Worktree: "worktree:" + SHA256String(manager.Boundary.Root)[:16],
		GitRevision: gitRevision, DirtyFingerprint: dirty,
		ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		CreatedAt: createdAt, Runtime: runtimeIdentity,
		DocumentCount: len(inventory.Documents), ChunkCount: len(inventory.Chunks),
		SourceStates:    append([]SourceState(nil), inventory.SourceStates...),
		DirectoryStates: append([]SourceState(nil), inventory.DirectoryStates...),
	}
	if err := populateDatabase(
		ctx, db, generation, inventory, manager.Manifest, previousDatabase,
	); err != nil {
		return Generation{}, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return Generation{}, err
	}
	if err := db.Close(); err != nil {
		return Generation{}, err
	}
	if _, err := os.Stat(finalPath); err == nil {
		quarantine := finalPath + ".corrupt-" +
			SHA256String(fmt.Sprintf("%s-%d", finalPath, time.Now().UnixNano()))[:12]
		_ = replaceFile(finalPath, quarantine)
	}
	if err := replaceFile(tempPath, finalPath); err != nil {
		return Generation{}, fmt.Errorf("publish immutable generation: %w", err)
	}
	if err := verifyDatabase(finalPath); err != nil {
		return Generation{}, err
	}
	if err := AtomicWriteJSON(filepath.Join(manager.CacheRoot, "current.json"), generation, 0o600); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func createSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE documents (
			id TEXT PRIMARY KEY, path TEXT NOT NULL UNIQUE, tier TEXT NOT NULL,
			title TEXT NOT NULL, content_hash TEXT NOT NULL, size INTEGER NOT NULL,
			source_kind TEXT NOT NULL DEFAULT '', finding_id TEXT NOT NULL DEFAULT '',
			campaign_id TEXT NOT NULL DEFAULT '', review_state TEXT NOT NULL DEFAULT '',
			validity TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE chunks (
			id TEXT PRIMARY KEY, document_id TEXT NOT NULL REFERENCES documents(id),
			path TEXT NOT NULL, tier TEXT NOT NULL, heading TEXT NOT NULL,
			start_line INTEGER NOT NULL, end_line INTEGER NOT NULL,
			byte_range INTEGER NOT NULL DEFAULT 0,
			start_byte INTEGER NOT NULL DEFAULT 0, end_byte INTEGER NOT NULL DEFAULT 0,
			content TEXT NOT NULL, content_hash TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '', context_hash TEXT NOT NULL DEFAULT '',
			parent_id TEXT, previous_id TEXT, next_id TEXT
		)`,
		`CREATE INDEX chunks_tier_path ON chunks(tier, path)`,
		// context is indexed, not UNINDEXED: carrying the document's claim
		// sentence into every chunk is what makes a chunk findable by what its
		// document asserts rather than only by the prose inside its own span.
		`CREATE VIRTUAL TABLE chunks_fts USING fts5(
			chunk_id UNINDEXED, path, heading, context, content,
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TABLE terms (
			term TEXT NOT NULL, chunk_id TEXT NOT NULL REFERENCES chunks(id),
			PRIMARY KEY(term, chunk_id)
		)`,
		`CREATE INDEX terms_chunk ON terms(chunk_id)`,
		`CREATE TABLE trigrams (
			trigram TEXT NOT NULL, chunk_id TEXT NOT NULL REFERENCES chunks(id),
			PRIMARY KEY(trigram, chunk_id)
		)`,
		`CREATE INDEX trigrams_chunk ON trigrams(chunk_id)`,
		`CREATE TABLE edges (
			source_id TEXT NOT NULL REFERENCES chunks(id),
			target_id TEXT NOT NULL REFERENCES chunks(id),
			kind TEXT NOT NULL,
			PRIMARY KEY(source_id, target_id, kind)
		)`,
		`CREATE INDEX edges_target ON edges(target_id)`,
		`CREATE TABLE vectors (
			model_id TEXT NOT NULL, model_spec_sha256 TEXT NOT NULL,
			chunk_id TEXT NOT NULL REFERENCES chunks(id),
			signature INTEGER NOT NULL, vector BLOB NOT NULL,
			PRIMARY KEY(model_id, chunk_id)
		)`,
		`CREATE INDEX vectors_signature ON vectors(model_id, signature)`,
		`CREATE TABLE findings (
			key TEXT PRIMARY KEY, id TEXT NOT NULL,
			document_id TEXT NOT NULL UNIQUE REFERENCES documents(id),
			campaign_id TEXT NOT NULL, kind TEXT NOT NULL, subject TEXT NOT NULL,
			claim TEXT NOT NULL, scope_json TEXT NOT NULL, evidence_grade TEXT NOT NULL,
			review_state TEXT NOT NULL, validity TEXT NOT NULL, projection TEXT NOT NULL,
			record_digest TEXT NOT NULL, body TEXT NOT NULL, source_class TEXT NOT NULL,
			verified_at TEXT NOT NULL DEFAULT '',
			questions_reviewed INTEGER NOT NULL
		)`,
		`CREATE UNIQUE INDEX findings_identity ON findings(campaign_id,id)`,
		`CREATE INDEX findings_filters ON findings(source_class,review_state,validity,campaign_id)`,
		`CREATE VIRTUAL TABLE finding_fts USING fts5(
			finding_key UNINDEXED, subject, claim, aliases, questions, scope, body,
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TABLE finding_queries (
			finding_key TEXT NOT NULL REFERENCES findings(key), kind TEXT NOT NULL,
			value TEXT NOT NULL, PRIMARY KEY(finding_key,kind,value)
		)`,
		`CREATE TABLE finding_terms (
			term TEXT NOT NULL, finding_key TEXT NOT NULL REFERENCES findings(key),
			kind TEXT NOT NULL, PRIMARY KEY(term,finding_key,kind)
		)`,
		`CREATE INDEX finding_terms_key ON finding_terms(finding_key)`,
		`CREATE TABLE finding_evidence (
			handle TEXT NOT NULL, finding_key TEXT NOT NULL REFERENCES findings(key),
			path TEXT NOT NULL, sha256 TEXT NOT NULL, start_line INTEGER NOT NULL DEFAULT 0,
			end_line INTEGER NOT NULL DEFAULT 0, object_key TEXT NOT NULL DEFAULT '',
			source_run TEXT NOT NULL DEFAULT '', PRIMARY KEY(handle,finding_key)
		)`,
		`CREATE INDEX finding_evidence_key ON finding_evidence(finding_key)`,
		`CREATE TABLE finding_relations (
			source_key TEXT NOT NULL REFERENCES findings(key), target_key TEXT NOT NULL,
			kind TEXT NOT NULL, PRIMARY KEY(source_key,target_key,kind)
		)`,
		`CREATE INDEX finding_relations_target ON finding_relations(target_key)`,
		`CREATE TABLE archive_reports (
			report_digest TEXT NOT NULL, document_id TEXT PRIMARY KEY REFERENCES documents(id),
			path TEXT NOT NULL, legacy_tier TEXT NOT NULL, normalized_finding_id TEXT
		)`,
		`CREATE INDEX archive_reports_digest ON archive_reports(report_digest)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func populateDatabase(
	ctx context.Context,
	db *sql.DB,
	generation Generation,
	inventory SourceInventory,
	manifest ModelManifest,
	previousDatabase string,
) error {
	previousAttached := false
	if previousDatabase != "" &&
		filepath.Clean(previousDatabase) != filepath.Clean(generation.Database) {
		if _, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS previous`, previousDatabase); err == nil {
			previousAttached = true
		}
	}
	reuseVectors := false
	if previousAttached {
		var previousBody string
		var previous Generation
		if err := db.QueryRowContext(
			ctx, `SELECT value FROM previous.metadata WHERE key='generation'`,
		).Scan(&previousBody); err == nil {
			if json.Unmarshal([]byte(previousBody), &previous) == nil &&
				previous.ModelFingerprint == generation.ModelFingerprint &&
				RuntimeContract(previous.Runtime) == RuntimeContract(generation.Runtime) {
				reuseVectors = true
			}
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	metadata, _ := json.Marshal(generation)
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES('generation',?)`, string(metadata)); err != nil {
		return err
	}
	reusableDocuments := map[string]bool{}
	for _, doc := range inventory.Documents {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO documents(id,path,tier,title,content_hash,size,source_kind,
			 finding_id,campaign_id,review_state,validity) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			doc.ID, doc.Path, doc.Tier, doc.Title, doc.ContentHash, doc.Size,
			doc.SourceKind, doc.FindingID, doc.CampaignID, doc.ReviewState, doc.Validity); err != nil {
			return err
		}
		if doc.SourceKind == "raw-report" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO archive_reports(
				report_digest,document_id,path,legacy_tier) VALUES(?,?,?,?)`,
				doc.ContentHash, doc.ID, doc.Path, doc.Tier); err != nil {
				return err
			}
		}
		if previousAttached {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM previous.documents
				WHERE id=? AND path=? AND tier=? AND content_hash=?`,
				doc.ID, doc.Path, doc.Tier, doc.ContentHash).Scan(&count); err == nil && count == 1 {
				reusableDocuments[doc.ID] = true
			}
		}
	}
	for _, chunk := range inventory.Chunks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(
			id,document_id,path,tier,heading,start_line,end_line,byte_range,start_byte,end_byte,content,content_hash,
			context,context_hash,
			parent_id,previous_id,next_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			chunk.ID, chunk.DocumentID, chunk.Path, chunk.Tier, chunk.Heading,
			chunk.StartLine, chunk.EndLine, chunk.ByteRange, chunk.StartByte, chunk.EndByte,
			chunk.Content, chunk.ContentHash,
			chunk.Context, chunk.ContextHash,
			nullable(chunk.ParentID), nullable(chunk.PreviousID), nullable(chunk.NextID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chunks_fts(chunk_id,path,heading,context,content) VALUES(?,?,?,?,?)`,
			chunk.ID, chunk.Path, chunk.Heading, chunk.Context, chunk.Content); err != nil {
			return err
		}
		reusedLexical := false
		reusableChunk := false
		if reusableDocuments[chunk.DocumentID] {
			var count int
			if err := tx.QueryRowContext(ctx,
				`SELECT count(*) FROM previous.chunks WHERE id=? AND content_hash=?`,
				chunk.ID, chunk.ContentHash).Scan(&count); err == nil && count == 1 {
				reusableChunk = true
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO terms(term,chunk_id)
					 SELECT term,chunk_id FROM previous.terms WHERE chunk_id=?`,
					chunk.ID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO trigrams(trigram,chunk_id)
					 SELECT trigram,chunk_id FROM previous.trigrams WHERE chunk_id=?`,
					chunk.ID); err != nil {
					return err
				}
				reusedLexical = true
			}
		}
		if !reusedLexical {
			for _, term := range extractTerms(chunk) {
				if _, err := tx.ExecContext(ctx,
					`INSERT OR IGNORE INTO terms(term,chunk_id) VALUES(?,?)`, term, chunk.ID); err != nil {
					return err
				}
				for _, trigram := range termTrigrams(term) {
					if _, err := tx.ExecContext(ctx,
						`INSERT OR IGNORE INTO trigrams(trigram,chunk_id) VALUES(?,?)`,
						trigram, chunk.ID); err != nil {
						return err
					}
				}
			}
		}
		for _, model := range manifest.Models {
			identity, executable := manifest.ExecutableModels[model.ID]
			if model.Role != "embedding" || !executable {
				continue
			}
			if reusableChunk && reuseVectors {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO vectors(model_id,model_spec_sha256,chunk_id,signature,vector)
					 SELECT model_id,model_spec_sha256,chunk_id,signature,vector
					 FROM previous.vectors
					 WHERE chunk_id=? AND model_id=? AND model_spec_sha256=?`,
					chunk.ID, identity.ID, identity.SpecSHA256); err != nil {
					return err
				}
				var copied int
				if err := tx.QueryRowContext(ctx,
					`SELECT count(*) FROM vectors WHERE chunk_id=? AND model_id=?
					 AND model_spec_sha256=?`,
					chunk.ID, identity.ID, identity.SpecSHA256).Scan(&copied); err != nil {
					return err
				}
				if copied == 1 {
					continue
				}
			}
			contextText := chunk.Path + "\n" + chunk.Heading + "\n" + chunk.Content
			vector, err := embeddingVector(identity, contextText)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO vectors(
				model_id,model_spec_sha256,chunk_id,signature,vector) VALUES(?,?,?,?,?)`,
				model.ID, model.SpecSHA256, chunk.ID, int64(vectorSignatureSlice(vector)),
				encodeVector(vector)); err != nil {
				return err
			}
		}
	}
	for _, edge := range inventory.Edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO edges(source_id,target_id,kind) VALUES(?,?,?)`,
			edge.Source, edge.Target, edge.Kind); err != nil {
			return err
		}
	}
	documentsByPath := map[string]SourceDocument{}
	for _, document := range inventory.Documents {
		if document.FindingID != "" {
			documentsByPath[document.Path] = document
		}
	}
	type indexedFinding struct {
		document FindingDocument
		source   SourceDocument
	}
	selected := map[string]indexedFinding{}
	for _, findingDocument := range inventory.Findings {
		findingDocument = normalizeFindingDocument(findingDocument)
		record := findingDocument.Record
		sourceDocument, indexed := documentsByPath[record.Path]
		if !indexed {
			return fmt.Errorf("finding %s/%s has no indexed source document", record.CampaignID, record.ID)
		}
		key := findingStorageKey(record.CampaignID, record.ID)
		if prior, duplicate := selected[key]; duplicate {
			if prior.document.Record.Digest != record.Digest {
				return fmt.Errorf(
					"finding %s/%s conflicts between %s and %s",
					record.CampaignID, record.ID, prior.source.Path, sourceDocument.Path)
			}
			priorRank := findingSourceSelectionRank(prior.source.Tier)
			candidateRank := findingSourceSelectionRank(sourceDocument.Tier)
			if priorRank == candidateRank && prior.source.Path != sourceDocument.Path {
				return fmt.Errorf(
					"finding %s/%s appears twice in %s provenance: %s and %s",
					record.CampaignID, record.ID, sourceDocument.Tier,
					prior.source.Path, sourceDocument.Path)
			}
			if candidateRank <= priorRank {
				continue
			}
		}
		selected[key] = indexedFinding{document: findingDocument, source: sourceDocument}
	}
	keys := make([]string, 0, len(selected))
	for key := range selected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		findingDocument := selected[key].document
		sourceDocument := selected[key].source
		record := findingDocument.Record
		scopeJSON, err := json.Marshal(record.Scope)
		if err != nil {
			return err
		}
		sourceClass := sourceDocument.Tier
		if _, err := tx.ExecContext(ctx, `INSERT INTO findings(
			key,id,document_id,campaign_id,kind,subject,claim,scope_json,evidence_grade,
			review_state,validity,projection,record_digest,body,source_class,verified_at,questions_reviewed)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, key, record.ID, sourceDocument.ID, record.CampaignID,
			record.Kind, record.Subject, record.Claim, string(scopeJSON), record.EvidenceGrade,
			record.ReviewState, record.Validity, record.Projection, record.Digest, record.Body,
			sourceClass, record.VerifiedAt, findingDocument.QuestionsReviewed); err != nil {
			return err
		}
		aliases := strings.Join(record.Aliases, "\n")
		questions := strings.Join(findingDocument.SyntheticQuestions, "\n")
		if _, err := tx.ExecContext(ctx, `INSERT INTO finding_fts(
			finding_key,subject,claim,aliases,questions,scope,body) VALUES(?,?,?,?,?,?,?)`,
			key, record.Subject, record.Claim, aliases, questions, string(scopeJSON), record.Body); err != nil {
			return err
		}
		for _, alias := range record.Aliases {
			if _, err := tx.ExecContext(ctx, `INSERT INTO finding_queries(finding_key,kind,value) VALUES(?,?,?)`, key, "alias", alias); err != nil {
				return err
			}
		}
		for _, question := range findingDocument.SyntheticQuestions {
			if _, err := tx.ExecContext(ctx, `INSERT INTO finding_queries(finding_key,kind,value) VALUES(?,?,?)`, key, "synthetic-question", question); err != nil {
				return err
			}
		}
		analysisText := strings.Join([]string{
			record.ID, record.Subject, record.Claim, aliases, questions, string(scopeJSON),
			strings.Join(record.Tags, " "), strings.Join(record.Subsystems, " "),
		}, "\n")
		for _, token := range AnalyzeIdentifierText(analysisText) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO finding_terms(term,finding_key,kind) VALUES(?,?,?)`, token.Value, key, token.Kind); err != nil {
				return err
			}
		}
		for _, evidence := range record.Evidence {
			if _, err := tx.ExecContext(ctx, `INSERT INTO finding_evidence(
				handle,finding_key,path,sha256,start_line,end_line,object_key,source_run)
				VALUES(?,?,?,?,?,?,?,?)`, EvidenceHandle(record.ID, evidence), key,
				evidence.Path, evidence.SHA256, evidence.StartLine, evidence.EndLine,
				evidence.ObjectKey, evidence.SourceRun); err != nil {
				return err
			}
		}
		for kind, targets := range FindingRelationSets(record.Relations) {
			for _, target := range targets {
				if _, err := tx.ExecContext(ctx, `INSERT INTO finding_relations(source_key,target_key,kind) VALUES(?,?,?)`, key, findingStorageKey(record.CampaignID, target), kind); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func findingSourceSelectionRank(sourceClass string) int {
	switch sourceClass {
	case "truth":
		return 4
	case "campaign", "provisional":
		return 3
	case "history":
		return 2
	default:
		return 1
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (manager IndexManager) LoadCurrent() (Generation, error) {
	generation, dbAbs, err := manager.loadCurrentPointer()
	if err != nil {
		return Generation{}, err
	}
	embedded, err := readGenerationMetadata(dbAbs)
	if err != nil {
		return Generation{}, fmt.Errorf(
			"read embedded generation metadata: %w", err)
	}
	embeddedDatabase, err := canonicalExistingPath(embedded.Database)
	if err != nil || embeddedDatabase != dbAbs {
		return Generation{}, errors.New(
			"embedded generation database identity does not match its file")
	}
	embedded.Database = embeddedDatabase
	if stableJSON(generation) != stableJSON(embedded) {
		return Generation{}, errors.New(
			"generation pointer does not match embedded database metadata")
	}
	if err := verifyDatabase(dbAbs); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func (manager IndexManager) corruptCurrentDatabase() string {
	generation, _, err := manager.loadCurrentPointer()
	if err != nil {
		return ""
	}
	if verifyDatabase(generation.Database) != nil {
		return generation.Database
	}
	return ""
}

func (manager IndexManager) loadCurrentPointer() (Generation, string, error) {
	cacheAbs, err := canonicalExistingPath(manager.CacheRoot)
	if err != nil {
		return Generation{}, "", fmt.Errorf("resolve cache root: %w", err)
	}
	pointerPath, err := containedOutputPath(cacheAbs, "current.json")
	if err != nil {
		return Generation{}, "", fmt.Errorf("resolve generation pointer: %w", err)
	}
	pointerInfo, err := os.Lstat(pointerPath)
	if err != nil {
		return Generation{}, "", err
	}
	if pointerInfo.Mode()&os.ModeSymlink != 0 ||
		!pointerInfo.Mode().IsRegular() ||
		pointerInfo.Size() < 1 || pointerInfo.Size() > maxSourceBytes {
		return Generation{}, "", errors.New(
			"generation pointer has an unsafe type or size")
	}
	body, err := os.ReadFile(pointerPath)
	if err != nil {
		return Generation{}, "", err
	}
	var generation Generation
	if err := decodeStrict(body, &generation); err != nil {
		return Generation{}, "", err
	}
	if generation.ID == "" || generation.Database == "" {
		return Generation{}, "", errors.New("invalid generation pointer")
	}
	if !strings.HasPrefix(generation.ID, "generation-") ||
		len(generation.ID) != len("generation-")+20 ||
		!hexDigestRE.MatchString(strings.Repeat("0", 44)+
			strings.TrimPrefix(generation.ID, "generation-")) {
		return Generation{}, "", errors.New("invalid generation pointer identity")
	}
	if !filepath.IsAbs(generation.Database) {
		return Generation{}, "", errors.New("generation database path must be absolute")
	}
	databaseInfo, err := os.Lstat(generation.Database)
	if err != nil {
		return Generation{}, "", err
	}
	if databaseInfo.Mode()&os.ModeSymlink != 0 ||
		!databaseInfo.Mode().IsRegular() {
		return Generation{}, "", errors.New(
			"generation database has an unsafe file type")
	}
	dbAbs, err := canonicalExistingPath(generation.Database)
	if err != nil {
		return Generation{}, "", fmt.Errorf("resolve generation database: %w", err)
	}
	generationsRoot := filepath.Join(cacheAbs, "generations")
	if !withinRoot(generationsRoot, dbAbs) {
		return Generation{}, "", errors.New(
			"generation database escapes the immutable generations directory")
	}
	if filepath.Clean(filepath.Dir(dbAbs)) != filepath.Clean(generationsRoot) ||
		filepath.Base(dbAbs) != generation.ID+".sqlite" {
		return Generation{}, "", errors.New(
			"generation database path does not match its pointer identity")
	}
	generation.Database = dbAbs
	return generation, dbAbs, nil
}

func verifyDatabase(path string) error {
	if path == "" {
		return errors.New("database path is empty")
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check: %s", result)
	}
	for _, table := range []string{
		"metadata", "documents", "chunks", "chunks_fts",
		"terms", "trigrams", "edges", "vectors", "findings", "finding_fts",
		"finding_queries", "finding_terms", "finding_evidence", "finding_relations",
		"archive_reports",
	} {
		var count int
		if err := db.QueryRow(
			`SELECT count(*) FROM sqlite_master
			 WHERE name=? AND type IN ('table','view')`,
			table,
		).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("required SQLite table %s is unavailable", table)
		}
	}
	var generationRows int
	if err := db.QueryRow(
		`SELECT count(*) FROM metadata WHERE key='generation'`,
	).Scan(&generationRows); err != nil || generationRows != 1 {
		return errors.New("SQLite generation metadata is missing or repeated")
	}
	var fts int
	if err := db.QueryRow("SELECT count(*) FROM chunks_fts").Scan(&fts); err != nil {
		return fmt.Errorf("FTS5 index unavailable: %w", err)
	}
	return nil
}

func readGenerationMetadata(path string) (Generation, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return Generation{}, err
	}
	defer db.Close()
	var body string
	if err := db.QueryRow(`SELECT value FROM metadata WHERE key='generation'`).Scan(&body); err != nil {
		return Generation{}, err
	}
	if len(body) < 1 || int64(len(body)) > maxSourceBytes {
		return Generation{}, errors.New(
			"embedded generation metadata has an unsafe size")
	}
	var generation Generation
	if err := decodeStrict([]byte(body), &generation); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func gitState(root string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	revision := runGit(ctx, root, "rev-parse", "HEAD")
	if revision == "" {
		return "unversioned", "unversioned"
	}
	status := runGit(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal")
	if status == "" {
		return revision, "clean"
	}
	return revision, "sha256:" + SHA256String(status)
}

func runGit(ctx context.Context, root string, args ...string) string {
	commandArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	body, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func MachineCacheRoot(boundary Boundary) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("XDG_CACHE_HOME")
	}
	if base == "" {
		if userCache, err := os.UserCacheDir(); err == nil {
			base = userCache
		}
	}
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "re-discipline", "worktrees", SHA256String(boundary.Root)[:20], "knowledge")
}

func ResolveCacheRoot(boundary Boundary, explicit string) (string, error) {
	projectBase := filepath.Join(boundary.Root, ".re-discipline", "cache")
	machineBase := MachineCacheRoot(boundary)
	if explicit != "" {
		if strings.ContainsRune(explicit, '\x00') {
			return "", errors.New("cache root contains invalid character")
		}
		var candidate string
		if filepath.IsAbs(explicit) {
			candidate = filepath.Clean(explicit)
		} else {
			clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(explicit, "\\", "/")))
			if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return "", errors.New("cache root traversal is forbidden")
			}
			candidate = filepath.Join(boundary.Root, clean)
		}
		resolvedParent, err := evalNearestExisting(filepath.Dir(candidate))
		if err != nil {
			return "", err
		}
		resolvedCandidate := filepath.Join(resolvedParent, filepath.Base(candidate))
		projectAllowed, err := evalNearestExisting(projectBase)
		if err != nil {
			return "", err
		}
		machineAllowed, err := evalNearestExisting(machineBase)
		if err != nil {
			return "", err
		}
		if !withinRoot(projectAllowed, resolvedCandidate) &&
			!withinRoot(machineAllowed, resolvedCandidate) {
			return "", errors.New("explicit cache root lacks a project or deterministic machine-local grant")
		}
		return resolvedCandidate, nil
	}
	project := DefaultCacheRoot(boundary)
	parent := filepath.Dir(project)
	resolvedParent, resolveErr := evalNearestExisting(parent)
	if resolveErr == nil && withinRoot(boundary.Root, resolvedParent) {
		mkdirErr := os.MkdirAll(parent, 0o700)
		probe, err := os.CreateTemp(parent, ".write-probe-*")
		if mkdirErr != nil {
			err = mkdirErr
		}
		if err == nil {
			name := probe.Name()
			probe.Close()
			os.Remove(name)
			return project, nil
		}
	}
	return MachineCacheRoot(boundary), nil
}

func evalNearestExisting(path string) (string, error) {
	missing := []string{}
	current := filepath.Clean(path)
	for {
		resolved, err := canonicalExistingPath(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func RuntimePlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func sqliteReadOnlyDSN(path string) string {
	absolute, _ := filepath.Abs(path)
	slash := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	uri := url.URL{Scheme: "file", Path: slash}
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func mustDigest(value any) string {
	digest, err := CanonicalDigest(value)
	if err != nil {
		panic(err)
	}
	return digest
}
