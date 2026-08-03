package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sha256IdentityRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var profileIdentityRE = regexp.MustCompile(
	`^[a-z0-9]+(?:-[a-z0-9]+)*(?::[a-z0-9]+(?:-[a-z0-9]+)*)?$`,
)
var hexDigestRE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var modelRevisionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// IndexingSettingsDigest digests exactly the settings that decide which files
// enter the index: the source-class toggles and any additional declared
// classes. It is deliberately narrower than the settings file.
//
// A budget or telemetry change alters what a response carries, not what the
// index contains. Mixing those in would rebuild the whole corpus every time a
// maintainer edited a token ceiling, which teaches people to distrust the
// rebuild rather than to trust the setting.
func IndexingSettingsDigest(settings KnowledgeSettings) string {
	normalized := settings.Sources
	additional := append([]AdditionalSource(nil), normalized.Additional...)
	sort.Slice(additional, func(i, j int) bool {
		if additional[i].Path != additional[j].Path {
			return additional[i].Path < additional[j].Path
		}
		if additional[i].Tier != additional[j].Tier {
			return additional[i].Tier < additional[j].Tier
		}
		return additional[i].Pattern < additional[j].Pattern
	})
	normalized.Additional = additional
	digest, err := CanonicalDigest(struct {
		Sources SourceSettings `json:"sources"`
	}{normalized})
	if err != nil {
		// A digest that cannot be computed must never compare equal to a
		// recorded one, or the failure becomes indistinguishable from
		// freshness.
		return "unavailable:" + err.Error()
	}
	return digest
}

func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		SchemaVersion:         BootstrapSchemaVersion,
		CampaignSchemaVersion: CampaignSchemaVersion,
		State: StateConfig{
			ActiveRoot: "active", ArchiveRoot: "docs/history/campaigns",
			LockFile: ".re-discipline/state/write.lock", Recovery: "replay-and-verify",
			GeneratedViewMaxItems: 24,
		},
		Authority: AuthorityConfig{
			ManagerRoles:      []string{"manager", "user"},
			CuratorWrites:     []string{"curator-run", "intake"},
			DirectStateWrites: false, TruthProjection: "closure-only",
		},
		Context: ContextConfig{
			ManagerCardTokens: 6144, DrafterCardTokens: 3072,
			MaxCards: 16, MaxExpansionBytes: 32768, LeaseMode: "memory-only",
		},
		Payload: PayloadConfig{
			CreateLazily: true, MaxInlineBytes: 1048576, RequireRegistration: true,
		},
		ReviewLoad: ReviewLoadConfig{
			TargetMinutesPerPacket: 12, TargetPacketsPerSession: 6,
		},
		Closure: ClosureConfig{
			RequireRunCoverage: true, RequireFindingDisposition: true,
			RequireFileRetention: true, RequireArchiveVerification: true,
		},
		Memory: MemoryConfig{
			Mode:        "shared-only",
			WritePolicy: "proposal-only",
		},
		Knowledge: KnowledgeConfig{
			Enabled:        true,
			Profile:        "plugin:balanced-v1",
			SettingsFile:   "knowledge/policy.jsonc",
			ProjectProfile: "knowledge/retrieval-profile.json",
		},
		Migration: MigrationConfig{Mode: "explicit-only", LegacyReaders: "migrator-only"},
	}
}

func DefaultKnowledgeSettings() KnowledgeSettings {
	return KnowledgeSettings{
		SchemaVersion: SettingsSchemaVersion,
		Sources: SourceSettings{
			Truth: true, HistoryFindings: true, Backlog: true,
			ActiveFindings: true, SharedMemory: true,
			ReportFallback: true,
		},
		Models:    ModelSettings{Execution: "local"},
		Telemetry: Telemetry{Mode: "metrics-only"},
		// The response envelope is charged against the same budget as the
		// passages it introduces. It measures about 332 tokens, so a 1024
		// search budget left roughly 692 for content against a median result
		// cost of 393, and retrieval behaved as top-1.
		Budgets: BudgetSettings{
			SearchTokens: 3072, ManagerContextTokens: 6144,
			DrafterContextTokens: 3072, MaxPassages: 16, MaxBytes: 32768,
		},
		Archive: ArchiveSettings{
			ReportFallbackUntilMeasured: true, NormalizationTriggerHits: 3,
			FallbackMode: "default-fallback",
		},
	}
}

func decodeStrict(data []byte, target any) error {
	if err := validateUniqueJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("invalid trailing JSON: %w", err)
		}
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return nil
}

func validateUniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, "$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return fmt.Errorf("invalid trailing JSON: %w", err)
		}
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, location string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key at %s is not a string", location)
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q at %s", key, location)
			}
			seen[key] = true
			if err := consumeJSONValue(decoder, location+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("unterminated JSON object at %s", location)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := consumeJSONValue(decoder, fmt.Sprintf("%s[%d]", location, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("unterminated JSON array at %s", location)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, location)
	}
	return nil
}

func StripJSONComments(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	lineComment := false
	blockComment := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
				out = append(out, c)
			} else {
				out = append(out, ' ')
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				out = append(out, ' ', ' ')
				i++
				blockComment = false
			} else if c == '\n' {
				out = append(out, '\n')
			} else {
				out = append(out, ' ')
			}
			continue
		}
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/':
				out = append(out, ' ', ' ')
				i++
				lineComment = true
				continue
			case '*':
				out = append(out, ' ', ' ')
				i++
				blockComment = true
				continue
			}
		}
		out = append(out, c)
	}
	if inString {
		return nil, fmt.Errorf("unterminated JSON string")
	}
	if blockComment {
		return nil, fmt.Errorf("unterminated block comment")
	}
	return out, nil
}

func LoadConfiguration(root string) Configuration {
	result := Configuration{
		Bootstrap: DefaultBootstrapConfig(),
		Settings:  DefaultKnowledgeSettings(),
		Valid:     true,
	}
	boundary, boundaryErr := NewBoundary(root)
	if boundaryErr != nil {
		result.Valid = false
		result.Unsafe = true
		result.Errors = append(result.Errors,
			"resolve project control-plane boundary: "+boundaryErr.Error())
		return result
	}
	configBody, err := readProjectControlFile(
		boundary, ".re-discipline/config.json",
	)
	if err != nil {
		result.Valid = false
		result.Unsafe = !os.IsNotExist(err)
		result.Errors = append(result.Errors, "missing or unreadable .re-discipline/config.json: "+err.Error())
		return result
	}
	var bootstrap BootstrapConfig
	if err := decodeStrict(configBody, &bootstrap); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "invalid .re-discipline/config.json: "+err.Error())
		return result
	}
	if err := ValidateBootstrap(bootstrap); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.Bootstrap = bootstrap

	settingsBody, err := readProjectControlFile(
		boundary,
		filepath.ToSlash(filepath.Join(".re-discipline",
			filepath.FromSlash(bootstrap.Knowledge.SettingsFile))),
	)
	if err != nil {
		result.Valid = false
		result.Unsafe = !os.IsNotExist(err)
		result.Errors = append(result.Errors, "missing or unreadable knowledge settings: "+err.Error())
		return result
	}
	settingsBody, err = StripJSONComments(settingsBody)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "invalid knowledge.jsonc: "+err.Error())
		return result
	}
	var rawSettings struct {
		Schema string `json:"$schema"`
		KnowledgeSettings
	}
	if err := decodeStrict(settingsBody, &rawSettings); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "invalid knowledge.jsonc: "+err.Error())
		return result
	}
	if err := ValidateSettings(rawSettings.KnowledgeSettings); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.Settings = rawSettings.KnowledgeSettings
	nativeEnabled := bootstrap.Memory.Mode != "shared-only"
	for _, adapter := range []struct {
		path      string
		reconcile func([]byte, bool) ([]byte, error)
	}{
		{".claude/settings.json", reconcileClaudeMemoryPolicy},
		{".codex/config.toml", reconcileCodexMemoryPolicy},
	} {
		body, err := readProjectControlFile(boundary, adapter.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			result.Valid = false
			result.Unsafe = true
			result.Errors = append(result.Errors,
				fmt.Sprintf("unsafe manager adapter %s: %v", adapter.path, err))
			continue
		}
		reconciled, err := adapter.reconcile(body, nativeEnabled)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("invalid manager adapter %s: %v", adapter.path, err))
			continue
		}
		if !bytes.Equal(body, reconciled) {
			result.Valid = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("manager adapter %s does not match memory mode %s",
					adapter.path, bootstrap.Memory.Mode))
		}
	}
	return result
}

func ValidateBootstrap(config BootstrapConfig) error {
	if config.SchemaVersion != BootstrapSchemaVersion {
		return fmt.Errorf("unsupported bootstrap schemaVersion %d", config.SchemaVersion)
	}
	if config.CampaignSchemaVersion != CampaignSchemaVersion {
		return fmt.Errorf("unsupported campaignSchemaVersion %d", config.CampaignSchemaVersion)
	}
	if config.State.ActiveRoot != "active" || config.State.ArchiveRoot != "docs/history/campaigns" ||
		config.State.LockFile != ".re-discipline/state/write.lock" ||
		config.State.Recovery != "replay-and-verify" ||
		config.State.GeneratedViewMaxItems < 4 || config.State.GeneratedViewMaxItems > 100 {
		return errors.New("state configuration must use the canonical roots, lock, recovery policy, and bounded view size")
	}
	if stableJSON(SortedUnique(config.Authority.ManagerRoles)) != stableJSON([]string{"manager", "user"}) ||
		stableJSON(SortedUnique(config.Authority.CuratorWrites)) != stableJSON([]string{"curator-run", "intake"}) ||
		config.Authority.DirectStateWrites || config.Authority.TruthProjection != "closure-only" {
		return errors.New("authority configuration must enforce engine-only writes and closure-only truth")
	}
	if config.Context.ManagerCardTokens < 512 || config.Context.ManagerCardTokens > 8192 ||
		config.Context.DrafterCardTokens < 512 || config.Context.DrafterCardTokens > 4096 ||
		config.Context.MaxCards < 1 || config.Context.MaxCards > 50 ||
		config.Context.MaxExpansionBytes < 4096 || config.Context.MaxExpansionBytes > 262144 ||
		!validOne(config.Context.LeaseMode, "none", "memory-only") {
		return errors.New("context configuration is outside safe bounds")
	}
	if !config.Payload.CreateLazily || config.Payload.MaxInlineBytes < 4096 ||
		!config.Payload.RequireRegistration {
		return errors.New("payload configuration must be lazy, bounded, and registration-gated")
	}
	if config.ReviewLoad.TargetMinutesPerPacket < 1 || config.ReviewLoad.TargetMinutesPerPacket > 240 ||
		config.ReviewLoad.TargetPacketsPerSession < 1 || config.ReviewLoad.TargetPacketsPerSession > 100 {
		return errors.New("review-load targets must be 1..240 minutes per packet and 1..100 packets per session")
	}
	if !config.Closure.RequireRunCoverage || !config.Closure.RequireFindingDisposition ||
		!config.Closure.RequireFileRetention || !config.Closure.RequireArchiveVerification {
		return errors.New("closure configuration cannot disable required gates")
	}
	if config.Knowledge.SettingsFile != "knowledge/policy.jsonc" ||
		config.Knowledge.ProjectProfile != "knowledge/retrieval-profile.json" {
		return fmt.Errorf("bootstrap paths must use the managed knowledge directory")
	}
	if config.Knowledge.Profile != "plugin:balanced-v1" {
		return fmt.Errorf("unsupported requested profile %q", config.Knowledge.Profile)
	}
	switch config.Memory.Mode {
	case "shared-only", "hybrid", "native":
	default:
		return fmt.Errorf("unsupported memory mode %q", config.Memory.Mode)
	}
	if config.Memory.WritePolicy != "proposal-only" {
		return fmt.Errorf("shared memory writePolicy must be proposal-only")
	}
	if config.Migration.Mode != "explicit-only" || config.Migration.LegacyReaders != "migrator-only" {
		return errors.New("migration must be explicit and the migrator must be the sole legacy reader")
	}
	return nil
}

func ValidateSettings(settings KnowledgeSettings) error {
	if settings.SchemaVersion != SettingsSchemaVersion {
		return fmt.Errorf("unsupported knowledge settings schemaVersion %d", settings.SchemaVersion)
	}
	if settings.Models.Execution != "local" {
		return fmt.Errorf("tracked settings may only select local model execution")
	}
	if settings.Telemetry.Mode != "off" && settings.Telemetry.Mode != "metrics-only" {
		return fmt.Errorf("telemetry mode must be off or metrics-only")
	}
	if len(settings.Sources.Additional) > 32 {
		return fmt.Errorf("at most 32 additional source classes are allowed")
	}
	additionalSeen := map[string]bool{}
	for index, source := range settings.Sources.Additional {
		normalized := strings.ReplaceAll(source.Path, "\\", "/")
		clean := path.Clean(normalized)
		// IsForbiddenSource includes the measurement-receipt root. A project
		// cannot turn benchmark output into indexed evidence by declaring it as
		// an additional source class.
		if normalized == "" || normalized != clean || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) ||
			IsForbiddenSource(clean) || IsForbiddenSource(clean+"/probe.md") {
			return fmt.Errorf("additional source %d has an unsafe project-relative path", index)
		}
		if source.Pattern == "" || strings.ContainsAny(source.Pattern, `/\`) ||
			strings.Contains(source.Pattern, "..") ||
			!strings.HasSuffix(strings.ToLower(source.Pattern), ".md") {
			return fmt.Errorf("additional source %d must use a local Markdown filename pattern", index)
		}
		if _, err := filepath.Match(source.Pattern, "probe.md"); err != nil {
			return fmt.Errorf("additional source %d has an invalid pattern: %w", index, err)
		}
		if !validOne(source.Tier, "asset", "profile", "playbook") {
			return fmt.Errorf(
				"additional source %d must use asset, profile, or playbook", index)
		}
		key := clean + "\x00" + source.Pattern + "\x00" + source.Tier
		if additionalSeen[key] {
			return fmt.Errorf("additional source classes must be unique")
		}
		additionalSeen[key] = true
	}
	b := settings.Budgets
	if b.SearchTokens < 128 || b.SearchTokens > 4096 ||
		b.ManagerContextTokens < 512 || b.ManagerContextTokens > 8192 ||
		b.DrafterContextTokens < 512 || b.DrafterContextTokens > 4096 ||
		b.MaxPassages < 1 || b.MaxPassages > 50 ||
		b.MaxBytes < 4096 || b.MaxBytes > 262144 {
		return fmt.Errorf("knowledge budgets are outside safe bounds")
	}
	if settings.Archive.NormalizationTriggerHits < 1 || settings.Archive.NormalizationTriggerHits > 1000000 {
		return fmt.Errorf("archive normalizationTriggerHits is outside safe bounds")
	}
	mode := settings.Archive.FallbackMode
	if mode == "" {
		mode = "default-fallback"
	}
	if !validOne(mode, "default-fallback", "opt-in") {
		return fmt.Errorf("unsupported archive fallbackMode %q", settings.Archive.FallbackMode)
	}
	if !settings.Sources.ReportFallback {
		return errors.New("archive reportFallback source must remain enabled for provenance expansion")
	}
	if settings.Archive.NormalizedBeatsRawReceipt != "" {
		if err := ValidateArchiveReceiptPath(settings.Archive.NormalizedBeatsRawReceipt); err != nil {
			return fmt.Errorf("archive normalizedBeatsRawReceipt: %w", err)
		}
	}
	if mode == "opt-in" {
		if settings.Archive.ReportFallbackUntilMeasured ||
			settings.Archive.NormalizedBeatsRawReceipt == "" {
			return errors.New("archive opt-in requires reportFallbackUntilMeasured=false and a ratified receipt path")
		}
	} else if !settings.Archive.ReportFallbackUntilMeasured {
		return errors.New("archive default fallback requires reportFallbackUntilMeasured=true")
	}
	return nil
}

func LoadProfile(assetRoot, projectRoot string, config Configuration) (RetrievalProfile, []string, error) {
	body, err := readContainedAsset(assetRoot, "profiles/balanced-v1.json")
	if err != nil {
		return RetrievalProfile{}, nil, fmt.Errorf("read plugin profile: %w", err)
	}
	var profile RetrievalProfile
	if err := decodeStrict(body, &profile); err != nil {
		return RetrievalProfile{}, nil, fmt.Errorf("parse plugin profile: %w", err)
	}
	warnings := []string{}
	if config.Valid {
		boundary, boundaryErr := NewBoundary(projectRoot)
		if boundaryErr != nil {
			return RetrievalProfile{}, nil, boundaryErr
		}
		projectRelative := filepath.ToSlash(filepath.Join(
			".re-discipline",
			filepath.FromSlash(config.Bootstrap.Knowledge.ProjectProfile),
		))
		projectPath := filepath.Join(boundary.Root, filepath.FromSlash(projectRelative))
		if _, statErr := os.Lstat(projectPath); statErr == nil {
			projectBody, projectErr := readProjectControlFile(boundary, projectRelative)
			if projectErr != nil {
				return RetrievalProfile{}, nil, fmt.Errorf(
					"read project retrieval profile: %w", projectErr)
			}
			var projectProfile RetrievalProfile
			if err := decodeStrict(projectBody, &projectProfile); err != nil {
				warnings = append(warnings, "invalid project retrieval profile; using plugin baseline: "+err.Error())
			} else if err := ValidateProfile(projectProfile); err != nil {
				warnings = append(warnings, "unsafe project retrieval profile; using plugin baseline: "+err.Error())
			} else if stableJSON(semanticProfile(projectProfile)) == stableJSON(semanticProfile(profile)) {
				profile = projectProfile
			} else if err := ValidateProjectProfileApproval(projectProfile); err != nil {
				warnings = append(warnings, "unratified project retrieval profile; using plugin baseline: "+err.Error())
			} else {
				profile = projectProfile
			}
		} else if !os.IsNotExist(statErr) {
			return RetrievalProfile{}, nil, fmt.Errorf(
				"inspect project retrieval profile: %w", statErr)
		}
	}
	if err := ValidateProfile(profile); err != nil {
		return RetrievalProfile{}, warnings, err
	}
	return profile, warnings, nil
}

func ValidateProjectProfileApproval(profile RetrievalProfile) error {
	if profile.BaseProfile == "" || profile.Approval == nil {
		return fmt.Errorf("changed project profile lacks baseProfile and approval")
	}
	expectedApprovalKeys := map[string]bool{
		"decision": true, "explicitUserApproval": true, "approvedAt": true,
		"profileDigest": true, "benchmarkMatrixDigest": true,
		"corpusFingerprint": true, "evalFingerprint": true,
		"modelFingerprint": true, "runtimeContract": true,
		"calibrationReportDigest": true, "candidateDigest": true,
	}
	if len(profile.Approval) != len(expectedApprovalKeys) {
		return fmt.Errorf("project approval receipt has unsupported or missing fields")
	}
	for key := range profile.Approval {
		if !expectedApprovalKeys[key] {
			return fmt.Errorf("project approval receipt has unsupported field %q", key)
		}
	}
	decision, _ := profile.Approval["decision"].(string)
	explicit, _ := profile.Approval["explicitUserApproval"].(bool)
	approvedAt, _ := profile.Approval["approvedAt"].(string)
	profileDigest, _ := profile.Approval["profileDigest"].(string)
	matrixDigest, _ := profile.Approval["benchmarkMatrixDigest"].(string)
	corpusFingerprint, _ := profile.Approval["corpusFingerprint"].(string)
	evalFingerprint, _ := profile.Approval["evalFingerprint"].(string)
	modelFingerprint, _ := profile.Approval["modelFingerprint"].(string)
	reportDigest, _ := profile.Approval["calibrationReportDigest"].(string)
	candidateDigest, _ := profile.Approval["candidateDigest"].(string)
	if decision != "promoted" || !explicit {
		return fmt.Errorf("project profile has no explicit promoted decision")
	}
	if _, err := time.Parse(time.RFC3339, approvedAt); err != nil {
		return fmt.Errorf("project approval approvedAt is missing or malformed")
	}
	for name, value := range map[string]string{
		"profileDigest": profileDigest, "benchmarkMatrixDigest": matrixDigest,
		"corpusFingerprint": corpusFingerprint, "evalFingerprint": evalFingerprint,
		"modelFingerprint":        modelFingerprint,
		"calibrationReportDigest": reportDigest, "candidateDigest": candidateDigest,
	} {
		if !strings.HasPrefix(value, "sha256:") || len(value) != 71 {
			return fmt.Errorf("project approval %s is missing or malformed", name)
		}
	}
	runtimeBody, err := json.Marshal(profile.Approval["runtimeContract"])
	if err != nil {
		return fmt.Errorf("project approval runtime contract is malformed")
	}
	var runtime RuntimeContractIdentity
	if err := decodeStrict(runtimeBody, &runtime); err != nil ||
		runtime.Implementation != "re-discipline-knowledge-go" ||
		runtime.Version != RuntimeVersion ||
		runtime.GoVersion == "" ||
		runtime.CompiledBuildID != "source-contract-"+RuntimeVersion ||
		runtime.SQLiteDriver != "modernc.org/sqlite@v1.54.0" ||
		runtime.SQLiteVersion == "" || runtime.SQLiteBuild == "" ||
		runtime.NumericalBackend != "fixed-int64-v1" ||
		runtime.TieBreaker != "score-desc,path-asc,start-line-asc,chunk-id-asc" {
		return fmt.Errorf("project approval runtime contract is missing or unsupported")
	}
	computed, err := approvedProfileDigest(profile)
	if err != nil || computed != profileDigest {
		return fmt.Errorf("project profile content hash does not match approval")
	}
	matrix := []map[string]string{}
	for _, row := range profile.EffectiveProfiles {
		if row.Benchmark.Status != "passed" {
			return fmt.Errorf("project effective profile %q is not independently passed", row.Name)
		}
		matrix = append(matrix, map[string]string{
			"name": row.Name, "digest": row.Benchmark.Digest,
			"status": row.Benchmark.Status, "suite": row.Benchmark.Suite,
		})
	}
	sort.Slice(matrix, func(i, j int) bool { return matrix[i]["name"] < matrix[j]["name"] })
	computedMatrix, _ := CanonicalDigest(matrix)
	if computedMatrix != matrixDigest {
		return fmt.Errorf("project profile benchmark matrix digest mismatch")
	}
	return nil
}

func approvedProfileDigest(profile RetrievalProfile) (string, error) {
	copyProfile := profile
	copyProfile.Approval = cloneAnyMap(profile.Approval)
	copyProfile.Approval["profileDigest"] = ""
	// Approval contains nested interface values. Normalize them through the
	// exact wire representation so the digest is identical before and after a
	// strict JSON round trip.
	body, err := json.Marshal(copyProfile)
	if err != nil {
		return "", err
	}
	var normalized RetrievalProfile
	if err := decodeStrict(body, &normalized); err != nil {
		return "", err
	}
	return CanonicalDigest(normalized)
}

func cloneAnyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func ValidateProfile(profile RetrievalProfile) error {
	if profile.SchemaVersion != 1 ||
		!profileIdentityRE.MatchString(profile.ProfileID) ||
		strings.TrimSpace(profile.Description) == "" ||
		len(profile.Description) > 2000 ||
		len(profile.EffectiveProfiles) == 0 ||
		len(profile.EffectiveProfiles) > 16 {
		return fmt.Errorf("retrieval profile identity or effective profiles are missing")
	}
	if profile.BaseProfile != "" &&
		!profileIdentityRE.MatchString(profile.BaseProfile) {
		return fmt.Errorf("retrieval profile baseProfile is malformed")
	}
	names := map[string]bool{}
	digests := map[string]bool{}
	baselineCount, denseCount := 0, 0
	for _, row := range profile.EffectiveProfiles {
		if !managedSlugRE.MatchString(row.Name) || names[row.Name] ||
			len(row.Description) > 2000 {
			return fmt.Errorf("effective profile names must be unique and nonempty")
		}
		names[row.Name] = true
		laneSet := map[string]bool{}
		for _, lane := range row.Lanes {
			if laneSet[lane] {
				return fmt.Errorf("effective profile %q repeats retrieval lane %q", row.Name, lane)
			}
			switch lane {
			case "exact", "fts", "graph", "dense":
				laneSet[lane] = true
			default:
				return fmt.Errorf("unsupported retrieval lane %q", lane)
			}
		}
		for lane, weight := range row.Weights {
			switch lane {
			case "exact", "fts", "graph", "dense":
			default:
				return fmt.Errorf("effective profile %q has unknown weight %q", row.Name, lane)
			}
			if weight < 0 || weight > 100 {
				return fmt.Errorf("effective profile %q has out-of-range %s weight", row.Name, lane)
			}
		}
		for _, lane := range []string{"exact", "fts", "graph", "dense"} {
			if _, ok := row.Weights[lane]; !ok {
				return fmt.Errorf("effective profile %q lacks %s weight", row.Name, lane)
			}
		}
		if !laneSet["exact"] || !laneSet["fts"] || !laneSet["graph"] {
			return fmt.Errorf("effective profile %q omits required lexical safety lanes", row.Name)
		}
		if laneSet["dense"] != (row.Requires.Embedding != nil) {
			return fmt.Errorf("effective profile %q dense lane and embedding requirement disagree", row.Name)
		}
		for _, lane := range []string{"exact", "fts", "graph"} {
			if row.Weights[lane] <= 0 {
				return fmt.Errorf("effective profile %q has non-positive %s weight", row.Name, lane)
			}
		}
		if laneSet["dense"] && row.Weights["dense"] <= 0 {
			return fmt.Errorf("effective profile %q has non-positive dense weight", row.Name)
		}
		if !laneSet["dense"] && row.Weights["dense"] != 0 {
			return fmt.Errorf("effective profile %q weights an inactive dense lane", row.Name)
		}
		if row.RRFK < 1 || row.RRFK > 1000 ||
			row.MaxPerDocument < 1 || row.MaxPerDocument > 20 ||
			row.Packing.MaxPassages < 1 || row.Packing.MaxPassages > 50 ||
			row.Packing.MaxBytes < 4096 || row.Packing.MaxBytes > 262144 {
			return fmt.Errorf("effective profile %q has unsafe bounds", row.Name)
		}
		if row.Benchmark.Suite != "packaged-conformance-v1" &&
			row.Benchmark.Suite != "project-calibration-v1" {
			return fmt.Errorf("effective profile %q has unsupported benchmark suite", row.Name)
		}
		if !sha256IdentityRE.MatchString(row.Benchmark.Digest) {
			return fmt.Errorf("effective profile %q lacks benchmark digest", row.Name)
		}
		switch row.Benchmark.Status {
		case "passed", "shadow", "stale", "failed":
		default:
			return fmt.Errorf("effective profile %q has unsupported benchmark status", row.Name)
		}
		if row.Benchmark.EvaluatedAt != "" {
			if _, err := time.Parse(time.RFC3339, row.Benchmark.EvaluatedAt); err != nil {
				return fmt.Errorf("effective profile %q has malformed evaluatedAt", row.Name)
			}
		}
		for name, fingerprint := range map[string]string{
			"eval":    row.Benchmark.EvalFingerprint,
			"corpus":  row.Benchmark.CorpusFingerprint,
			"model":   row.Benchmark.ModelFingerprint,
			"runtime": row.Benchmark.RuntimeFingerprint,
		} {
			if fingerprint != "" && !sha256IdentityRE.MatchString(fingerprint) {
				return fmt.Errorf(
					"effective profile %q has malformed %s fingerprint",
					row.Name, name,
				)
			}
		}
		if row.Benchmark.Status == "passed" {
			if row.Benchmark.EvaluatedAt == "" {
				return fmt.Errorf(
					"passed effective profile %q lacks evaluatedAt",
					row.Name,
				)
			}
			for name, fingerprint := range map[string]string{
				"eval":    row.Benchmark.EvalFingerprint,
				"corpus":  row.Benchmark.CorpusFingerprint,
				"model":   row.Benchmark.ModelFingerprint,
				"runtime": row.Benchmark.RuntimeFingerprint,
			} {
				if !sha256IdentityRE.MatchString(fingerprint) {
					return fmt.Errorf(
						"passed effective profile %q lacks a valid %s fingerprint",
						row.Name, name,
					)
				}
			}
		}
		if digests[row.Benchmark.Digest] {
			return fmt.Errorf("effective profiles must have independent benchmark digests")
		}
		digests[row.Benchmark.Digest] = true
		if row.Benchmark.Status != "passed" {
			continue
		}
		if row.Requires.Embedding == nil {
			baselineCount++
		} else {
			denseCount++
		}
	}
	if baselineCount != 1 || denseCount != 1 || len(profile.EffectiveProfiles) != 2 {
		return fmt.Errorf(
			"profile must contain exactly one passed dense and one passed model-free capability row")
	}
	return nil
}

func ValidateProfileModels(profile RetrievalProfile, manifest ModelManifest) error {
	models := map[string]ModelSpec{}
	for _, model := range manifest.Models {
		models[model.ID] = model
	}
	for _, row := range profile.EffectiveProfiles {
		if row.Requires.Embedding == nil {
			continue
		}
		model, ok := models[*row.Requires.Embedding]
		if !ok {
			return fmt.Errorf("profile %q references unmanifested model %q", row.Name, *row.Requires.Embedding)
		}
		if model.Role != "embedding" {
			return fmt.Errorf("profile %q uses model %q for the wrong role", row.Name, *row.Requires.Embedding)
		}
	}
	return nil
}

func LoadModelManifest(assetRoot string) (ModelManifest, error) {
	body, err := readContainedAsset(assetRoot, "models/manifest.json")
	if err != nil {
		return ModelManifest{}, err
	}
	var manifest ModelManifest
	if err := decodeStrict(body, &manifest); err != nil {
		return ModelManifest{}, err
	}
	if manifest.SchemaVersion != 1 || manifest.Runtime.Version != RuntimeVersion {
		return ModelManifest{}, fmt.Errorf("model manifest runtime version mismatch")
	}
	if manifest.Runtime.Implementation != "re-discipline-knowledge-go" ||
		manifest.Runtime.SQLiteDriver != "modernc.org/sqlite@v1.54.0" ||
		manifest.Runtime.NumericalBackend != "fixed-int64-v1" ||
		manifest.Runtime.TieBreaker != "score-desc,path-asc,start-line-asc,chunk-id-asc" {
		return ModelManifest{}, fmt.Errorf("model manifest runtime identity is unsupported")
	}
	for key, expected := range map[string]bool{
		"networkDownloads": false, "requireManifestEntry": true,
		"requireArtifactSha256": true, "requireLocalPathGrant": true,
	} {
		value, ok := manifest.ExternalModelPolicy[key].(bool)
		if !ok || value != expected {
			return ModelManifest{}, fmt.Errorf("external model policy %q is unsafe", key)
		}
	}
	if len(manifest.ExternalModelPolicy) != 4 {
		return ModelManifest{}, fmt.Errorf("external model policy contains unsupported fields")
	}
	manifest.ExecutableModels = map[string]ModelIdentity{}
	manifest.UnavailableModels = map[string]string{}
	if len(manifest.Models) != 1 {
		return ModelManifest{}, errors.New(
			"model manifest must contain exactly one embedding model")
	}
	seen := map[string]bool{}
	for _, model := range manifest.Models {
		if !profileIdentityRE.MatchString(model.ID) || seen[model.ID] ||
			!hexDigestRE.MatchString(model.SpecSHA256) ||
			model.NetworkRequired || !modelRevisionRE.MatchString(model.Revision) ||
			strings.TrimSpace(model.License) == "" ||
			len(model.Description) > 2000 || model.Dimensions < 1 ||
			model.Dimensions > 4096 {
			return ModelManifest{}, fmt.Errorf("invalid or network-enabled model manifest entry %q", model.ID)
		}
		if model.Role != "embedding" {
			return ModelManifest{}, fmt.Errorf("model %q has unsupported role %q", model.ID, model.Role)
		}
		switch model.Implementation {
		case "builtin", "bundled-local", "onnx-local":
		default:
			return ModelManifest{}, fmt.Errorf(
				"model %q has unsupported implementation %q", model.ID, model.Implementation)
		}
		if model.Implementation == "builtin" && model.License != "MIT" {
			return ModelManifest{}, fmt.Errorf(
				"builtin model %q must declare the exact SPDX license MIT", model.ID)
		}
		specPath := filepath.ToSlash(model.SpecFile)
		if filepath.IsAbs(model.SpecFile) || path.Clean(specPath) != specPath ||
			!strings.HasPrefix(specPath, "models/specs/") ||
			strings.Contains(specPath, "../") {
			return ModelManifest{}, fmt.Errorf("model %q has unsafe spec path", model.ID)
		}
		specBody, err := readContainedAsset(assetRoot, specPath)
		if err != nil {
			return ModelManifest{}, fmt.Errorf("read model spec %q: %w", model.ID, err)
		}
		if SHA256Bytes(specBody) != model.SpecSHA256 {
			return ModelManifest{}, fmt.Errorf("model spec checksum mismatch for %q", model.ID)
		}
		var declared struct {
			SchemaVersion   int    `json:"schemaVersion"`
			ID              string `json:"id"`
			Revision        string `json:"revision"`
			License         string `json:"license"`
			Dimensions      int    `json:"dimensions"`
			NetworkRequired bool   `json:"networkRequired"`
		}
		if err := json.Unmarshal(specBody, &declared); err != nil ||
			declared.SchemaVersion != 1 || declared.ID != model.ID ||
			declared.Revision != model.Revision ||
			declared.License != model.License || declared.NetworkRequired ||
			declared.Dimensions != model.Dimensions {
			return ModelManifest{}, fmt.Errorf("model spec identity mismatch for %q", model.ID)
		}
		if model.Implementation == "builtin" && model.Dimensions != featureDimensions {
			return ModelManifest{}, fmt.Errorf("builtin feature embedding dimensions mismatch for %q", model.ID)
		}
		if model.Implementation == "bundled-local" {
			artifactPath := filepath.ToSlash(model.ArtifactFile)
			if filepath.IsAbs(model.ArtifactFile) || path.Clean(artifactPath) != artifactPath ||
				!strings.HasPrefix(artifactPath, "models/artifacts/") ||
				strings.Contains(artifactPath, "../") ||
				!hexDigestRE.MatchString(model.ArtifactSHA256) {
				return ModelManifest{}, fmt.Errorf("bundled model %q has unsafe artifact identity", model.ID)
			}
			artifactBody, err := readContainedAsset(assetRoot, artifactPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					manifest.UnavailableModels[model.ID] = "artifact-missing"
				} else {
					return ModelManifest{}, fmt.Errorf(
						"read model artifact %q: %w", model.ID, err)
				}
			} else if SHA256Bytes(artifactBody) != model.ArtifactSHA256 {
				manifest.UnavailableModels[model.ID] = "artifact-checksum-mismatch"
			} else if err := registerBundledEmbedding(model, artifactBody); err != nil {
				manifest.UnavailableModels[model.ID] = "artifact-invalid"
			}
		}
		if model.Implementation == "onnx-local" &&
			!hexDigestRE.MatchString(model.ArtifactSHA256) {
			return ModelManifest{}, fmt.Errorf("external model %q lacks pinned artifact checksum", model.ID)
		}
		switch model.Implementation {
		case "builtin":
			manifest.ExecutableModels[model.ID] =
				modelIdentity(model, manifest.Runtime.NumericalBackend)
		case "bundled-local":
			if _, unavailable := manifest.UnavailableModels[model.ID]; !unavailable {
				manifest.ExecutableModels[model.ID] =
					modelIdentity(model, manifest.Runtime.NumericalBackend)
			}
		case "onnx-local":
			// A tracked manifest entry is not an execution grant for an
			// arbitrary machine-local runtime or artifact.
			manifest.UnavailableModels[model.ID] = "external-execution-not-granted"
		}
		seen[model.ID] = true
	}
	return manifest, nil
}

func readContainedAsset(assetRoot, relative string) ([]byte, error) {
	root, err := canonicalExistingPath(assetRoot)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("asset path may not be a symbolic link")
	}
	resolved, err := canonicalExistingPath(target)
	if err != nil {
		return nil, err
	}
	if !withinRoot(root, resolved) {
		return nil, errors.New("asset path escapes the plugin knowledge root")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("asset path is not a regular file")
	}
	return os.ReadFile(resolved)
}

func readProjectControlFile(boundary Boundary, relative string) ([]byte, error) {
	absolute, err := boundary.Resolve(relative, true)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		info.Size() < 1 || info.Size() > maxSourceBytes {
		return nil, errors.New("project control-plane path has an unsafe type or size")
	}
	return os.ReadFile(absolute)
}

func modelIdentity(model ModelSpec, numericalBackend string) ModelIdentity {
	return ModelIdentity{
		ID: model.ID, Revision: model.Revision, SpecSHA256: model.SpecSHA256,
		ArtifactSHA256: model.ArtifactSHA256, Implementation: model.Implementation,
		NumericalBackend: numericalBackend, Dimensions: model.Dimensions,
	}
}

func SelectEffectiveProfile(
	profile RetrievalProfile,
	manifest ModelManifest,
	runtime RuntimeIdentity,
) (SelectedProfile, error) {
	if len(manifest.Models) != 1 || manifest.Models[0].Role != "embedding" {
		return SelectedProfile{}, errors.New(
			"profile selection requires exactly one validated embedding manifest entry")
	}
	models := map[string]ModelIdentity{}
	for _, model := range manifest.Models {
		if identity, ok := manifest.ExecutableModels[model.ID]; ok {
			models[model.ID] = identity
			continue
		}
		// Preserve the pure selection helper for hand-constructed test
		// manifests while requiring loaded bundled artifacts to carry explicit
		// service-local availability.
		if manifest.ExecutableModels == nil && model.Implementation == "builtin" {
			models[model.ID] = modelIdentity(model, runtime.NumericalBackend)
		}
	}
	requested, err := CanonicalDigest(semanticProfile(profile))
	if err != nil {
		return SelectedProfile{}, err
	}
	rows := append([]EffectiveProfile(nil), profile.EffectiveProfiles...)
	sort.Slice(rows, func(i, j int) bool {
		left, right := capabilityPreference(rows[i]), capabilityPreference(rows[j])
		if left != right {
			return left < right
		}
		return rows[i].Name < rows[j].Name
	})
	preferredName := ""
	for _, row := range rows {
		if row.Benchmark.Status == "passed" {
			preferredName = row.Name
			break
		}
	}
	missingReasons := []string{}
	for _, row := range rows {
		if row.Benchmark.Status != "passed" {
			continue
		}
		activeModels := []ModelIdentity{}
		if row.Requires.Embedding != nil {
			identity, ok := models[*row.Requires.Embedding]
			if !ok {
				reason := manifest.UnavailableModels[*row.Requires.Embedding]
				if reason == "" {
					reason = "unavailable"
				}
				missingReasons = append(
					missingReasons,
					"embedding-"+reason+":"+*row.Requires.Embedding,
				)
				continue
			}
			activeModels = append(activeModels, identity)
		}
		identityInput := struct {
			Profile EffectiveProfile        `json:"profile"`
			Runtime RuntimeContractIdentity `json:"runtime"`
			Models  []ModelIdentity         `json:"models"`
		}{runtimeEffectiveProfile(row), RuntimeContract(runtime), activeModels}
		effective, err := CanonicalDigest(identityInput)
		if err != nil {
			return SelectedProfile{}, err
		}
		var fallback *string
		if row.Name != preferredName {
			reason := strings.Join(SortedUnique(missingReasons), ",")
			if reason == "" {
				reason = "predefined-degraded-profile-selected"
			}
			fallback = &reason
		}
		return SelectedProfile{
			RequestedIdentity: profile.ProfileID + "@" + requested,
			EffectiveIdentity: row.Name + "@" + effective,
			Effective:         row, ActiveLanes: append([]string(nil), row.Lanes...),
			Models: activeModels, FallbackReason: fallback, Runtime: runtime,
		}, nil
	}
	return SelectedProfile{}, fmt.Errorf("no independently benchmarked effective profile matches available capabilities")
}

func capabilityPreference(row EffectiveProfile) int {
	if row.Requires.Embedding != nil {
		return 0
	}
	return 1
}

func runtimeEffectiveProfile(row EffectiveProfile) EffectiveProfile {
	row = cloneEffectiveProfile(row)
	// Benchmark receipts and prose do not change execution. Excluding them
	// keeps the runtime identity stable when the same measured configuration is
	// re-evaluated or promoted with a different evidence receipt.
	row.Description = ""
	row.Benchmark = BenchmarkEvidence{}
	return row
}

func semanticProfile(profile RetrievalProfile) RetrievalProfile {
	profile.Schema = ""
	profile.Approval = nil
	profile.EffectiveProfiles = append([]EffectiveProfile(nil), profile.EffectiveProfiles...)
	for index := range profile.EffectiveProfiles {
		row := profile.EffectiveProfiles[index]
		row.Lanes = append([]string(nil), row.Lanes...)
		sort.Strings(row.Lanes)
		// Benchmark receipts and prose do not change execution, mirroring
		// runtimeEffectiveProfile. Without this, regenerating a packaged
		// conformance digest makes every project holding a copied profile
		// compare unequal, get flagged unratified, and lose benchmarking -
		// even though the two profiles are semantically identical.
		row.Benchmark = BenchmarkEvidence{}
		profile.EffectiveProfiles[index] = row
	}
	sort.Slice(profile.EffectiveProfiles, func(i, j int) bool {
		left, right := capabilityPreference(profile.EffectiveProfiles[i]),
			capabilityPreference(profile.EffectiveProfiles[j])
		if left != right {
			return left < right
		}
		return profile.EffectiveProfiles[i].Name < profile.EffectiveProfiles[j].Name
	})
	return profile
}
