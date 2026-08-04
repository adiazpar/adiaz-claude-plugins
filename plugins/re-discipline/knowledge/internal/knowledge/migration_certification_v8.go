package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const (
	// maxMigrationEvidenceBytes bounds certification evidence reads. The
	// corpus-scan bound (maxSourceBytes, 4 MiB) is too small here: the
	// runtime's own full 64-case project-benchmark-v1 report with per-case
	// observation rows measures ~7 MB on this project, so reusing the scan
	// bound made the retrieval gate reject the exact report the packaged
	// benchmark command produces. 16 MiB matches the external evidence
	// harness's capture bound; digests are still verified over exact bytes.
	maxMigrationEvidenceBytes int64 = 16 * 1024 * 1024

	migrationRetrievalCertificationSuite = "migration-retrieval-certification-v1"
	migrationBlindedEvaluationSuite      = "migration-blinded-agent-evaluation-v1"
	migrationHostConformanceSuite        = "migration-host-conformance-v1"
	migrationBlindedProtocol             = "Compare the same hidden migration task under legacy and compiled context. Record the exact request, answer, citations, opened sources, expansions, durability labels, unsupported claims, decisions, and context-token cost. The compiled arm must be factually and decision non-inferior, use no more context, preserve evidence traceability, and avoid full-corpus reads."
	migrationHostProtocol                = "Exercise the actual MCP, CLI, and every configured agent-host adapter with captured request, transport result, normalized semantic result, and expected failure. Require discovery, status, retrieval, expansion, role-boundary refusal, bounded recovery, and local fallback coverage with equivalent semantic digests."
)

// migrationMandatoryHosts are the surfaces every migrated project must prove:
// the MCP server, the CLI, and the Claude Code host. Codex is an optional
// host: a project that does not use it owes no Codex captures, but when Codex
// trials are present the complete Codex scenario matrix must pass and match
// cross-host semantic parity. Optional hosts that are absent are genuinely
// absent — they contribute no fingerprint and no empty placeholder.
var migrationMandatoryHosts = []string{"mcp", "cli", "claude"}

func migrationHostScenarios(host string) []string {
	switch host {
	case "mcp":
		return []string{"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery"}
	case "cli":
		return []string{"status", "retrieval", "expansion", "role-boundary", "bounded-recovery"}
	case "claude", "codex":
		return []string{"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery", "local-fallback"}
	default:
		return nil
	}
}

func migrationHostsPresent(trials []MigrationHostTrial) map[string]bool {
	present := map[string]bool{}
	for _, trial := range trials {
		present[trial.Host] = true
	}
	return present
}

// MigrationEvidenceArtifactReference binds a certification input to exact
// bytes in a regular, project-contained file. The gate verifier always
// re-reads the bytes; a caller-supplied digest is never evidence by itself.
type MigrationEvidenceArtifactReference struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// MigrationRetrievalGateEvidence identifies the final project benchmark,
// calibration, non-activated candidate profile, and blinded evaluation that
// jointly support the retrieval/context gate.
type MigrationRetrievalGateEvidence struct {
	SchemaVersion     int                                `json:"schemaVersion"`
	Suite             string                             `json:"suite"`
	Benchmark         MigrationEvidenceArtifactReference `json:"benchmark"`
	Calibration       MigrationEvidenceArtifactReference `json:"calibration"`
	CandidateProfile  MigrationEvidenceArtifactReference `json:"candidateProfile"`
	BlindedEvaluation MigrationEvidenceArtifactReference `json:"blindedEvaluation"`
	ResultDigest      string                             `json:"resultDigest"`
}

type MigrationBlindedAgentRequest struct {
	CaseID       string   `json:"caseId"`
	Query        string   `json:"query"`
	QueryClass   string   `json:"queryClass"`
	Role         string   `json:"role"`
	AllowedTiers []string `json:"allowedTiers"`
	TokenBudget  int      `json:"tokenBudget"`
}

type MigrationBlindedAgentResponse struct {
	AnswerClaims       []string `json:"answerClaims"`
	Decision           string   `json:"decision"`
	OpenedSourceIDs    []string `json:"openedSourceIds"`
	ContextCardHandles []string `json:"contextCardHandles"`
	ExpansionHandles   []string `json:"expansionHandles"`
	Citations          []string `json:"citations"`
	DurabilityLabels   []string `json:"durabilityLabels"`
	FullCorpusRead     bool     `json:"fullCorpusRead"`
	ContextTokens      int      `json:"contextTokens"`
	Error              string   `json:"error"`
}

// MigrationBlindedAgentCaseOutcome preserves one actual agent transcript and
// its independent score. Every benchmark holdout case must have both a legacy
// and a compiled-context trial; aggregate-only scores are rejected.
type MigrationBlindedAgentCaseOutcome struct {
	ID                     string                        `json:"id"`
	CaseID                 string                        `json:"caseId"`
	Condition              string                        `json:"condition"`
	Agent                  string                        `json:"agent"`
	Model                  string                        `json:"model"`
	AgentDigest            string                        `json:"agentDigest"`
	QueryDigest            string                        `json:"queryDigest"`
	BenchmarkOutcomeDigest string                        `json:"benchmarkOutcomeDigest"`
	Answerable             bool                          `json:"answerable"`
	Request                MigrationBlindedAgentRequest  `json:"request"`
	Response               MigrationBlindedAgentResponse `json:"response"`
	ResponseDigest         string                        `json:"responseDigest"`
	FactualAccuracy        float64                       `json:"factualAccuracy"`
	DecisionAccuracy       float64                       `json:"decisionAccuracy"`
	UnsupportedClaims      int                           `json:"unsupportedClaims"`
	EvidenceTracePassed    bool                          `json:"evidenceTracePassed"`
	Passed                 bool                          `json:"passed"`
	Digest                 string                        `json:"digest"`
}

type MigrationBlindedAgentEvaluation struct {
	SchemaVersion     int                                `json:"schemaVersion"`
	Suite             string                             `json:"suite"`
	TransactionID     string                             `json:"transactionId"`
	PlanDigest        string                             `json:"planDigest"`
	BenchmarkDigest   string                             `json:"benchmarkDigest"`
	CalibrationDigest string                             `json:"calibrationDigest"`
	ProtocolDigest    string                             `json:"protocolDigest"`
	Evaluator         string                             `json:"evaluator"`
	EvaluatorDigest   string                             `json:"evaluatorDigest"`
	Cases             []MigrationBlindedAgentCaseOutcome `json:"cases"`
	Passed            bool                               `json:"passed"`
	ResultDigest      string                             `json:"resultDigest"`
}

type MigrationHostFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MigrationHostTrial captures the transport request/result as well as the
// normalized semantic result used for parity. SemanticDigest and Digest are
// rederived by the verifier.
type MigrationHostTrial struct {
	ID             string                `json:"id"`
	Host           string                `json:"host"`
	Scenario       string                `json:"scenario"`
	Transport      string                `json:"transport"`
	Request        json.RawMessage       `json:"request"`
	Result         json.RawMessage       `json:"result"`
	Failure        *MigrationHostFailure `json:"failure"`
	ExitCode       int                   `json:"exitCode"`
	SemanticResult json.RawMessage       `json:"semanticResult"`
	SemanticDigest string                `json:"semanticDigest"`
	Passed         bool                  `json:"passed"`
	Digest         string                `json:"digest"`
}

type MigrationHostConformanceEvidence struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Suite          string               `json:"suite"`
	TransactionID  string               `json:"transactionId"`
	PlanDigest     string               `json:"planDigest"`
	ProtocolDigest string               `json:"protocolDigest"`
	Trials         []MigrationHostTrial `json:"trials"`
	Passed         bool                 `json:"passed"`
	ResultDigest   string               `json:"resultDigest"`
}

func migrationBlindedProtocolDigest() string {
	return "sha256:" + SHA256String(migrationBlindedProtocol)
}

func migrationHostProtocolDigest() string {
	return "sha256:" + SHA256String(migrationHostProtocol)
}

func migrationCanonicalRawDigest(body json.RawMessage) (string, any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "", nil, errors.New("captured JSON is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", nil, errors.New("captured JSON has a trailing value")
	}
	digest, err := CanonicalDigest(value)
	return digest, value, err
}

// SealMigrationBlindedAgentCaseOutcome derives every identity and digest in a
// captured blinded trial. It does not judge comparative non-inferiority; the
// gate verifier performs that check over the complete paired suite.

// migrationBlindedAnswerShapeCorrect reports whether a trial's answer shape
// matches what the case asks for. An answerable case must produce claims and
// citations. An abstention case is correct precisely when it produces neither:
// scoring abstentions by the answerable rule made a correct refusal
// indistinguishable from a failure, so a corpus with abstention holdouts could
// never satisfy the gate.
func migrationBlindedAnswerShapeCorrect(
	response MigrationBlindedAgentResponse,
	answerable bool,
) bool {
	if answerable {
		return len(response.AnswerClaims) > 0 && len(response.Citations) > 0
	}
	return len(response.AnswerClaims) == 0 && len(response.Citations) == 0
}

func SealMigrationBlindedAgentCaseOutcome(
	outcome *MigrationBlindedAgentCaseOutcome,
) error {
	if outcome == nil {
		return errors.New("blinded case outcome is nil")
	}
	agentDigest, err := CanonicalDigest(struct {
		Agent string `json:"agent"`
		Model string `json:"model"`
	}{outcome.Agent, outcome.Model})
	if err != nil {
		return err
	}
	outcome.AgentDigest = agentDigest
	outcome.QueryDigest = "sha256:" + SHA256String(outcome.Request.Query)
	outcome.ResponseDigest, err = CanonicalDigest(outcome.Response)
	if err != nil {
		return err
	}
	outcome.ID = StableID("MBE", outcome.CaseID, outcome.Condition,
		outcome.AgentDigest, outcome.QueryDigest)
	outcome.Passed = outcome.UnsupportedClaims == 0 && outcome.EvidenceTracePassed &&
		outcome.FactualAccuracy >= 0.5 && outcome.DecisionAccuracy >= 0.5 &&
		migrationBlindedAnswerShapeCorrect(outcome.Response, outcome.Answerable)
	outcome.Digest = ""
	outcome.Digest, err = CanonicalDigest(*outcome)
	return err
}

func SealMigrationBlindedAgentEvaluation(
	evaluation *MigrationBlindedAgentEvaluation,
) error {
	if evaluation == nil {
		return errors.New("blinded evaluation is nil")
	}
	evaluation.SchemaVersion = MigrationSchemaVersion
	evaluation.Suite = migrationBlindedEvaluationSuite
	evaluation.ProtocolDigest = migrationBlindedProtocolDigest()
	var err error
	evaluation.EvaluatorDigest, err = CanonicalDigest(struct {
		Evaluator string `json:"evaluator"`
		Protocol  string `json:"protocolDigest"`
	}{evaluation.Evaluator, evaluation.ProtocolDigest})
	if err != nil {
		return err
	}
	// The compiled arm carries the absolute bar: every compiled trial must
	// pass its own case. The legacy arm is the captured baseline that the
	// verifier's per-case non-inferiority comparison measures against; a
	// legacy miss is a true measurement of the predecessor runtime, not a
	// reason the migration cannot certify. Requiring the legacy arm to be
	// perfect would make this gate unsatisfiable for exactly the projects
	// whose retrieval the migration exists to improve.
	compiledTrials := 0
	evaluation.Passed = len(evaluation.Cases) > 0
	for index := range evaluation.Cases {
		if err := SealMigrationBlindedAgentCaseOutcome(&evaluation.Cases[index]); err != nil {
			return err
		}
		if evaluation.Cases[index].Condition == "compiled" {
			compiledTrials++
			evaluation.Passed = evaluation.Passed && evaluation.Cases[index].Passed
		}
	}
	evaluation.Passed = evaluation.Passed && compiledTrials > 0
	evaluation.ResultDigest = ""
	evaluation.ResultDigest, err = CanonicalDigest(*evaluation)
	return err
}

func SealMigrationHostTrial(trial *MigrationHostTrial) error {
	if trial == nil {
		return errors.New("host trial is nil")
	}
	requestDigest, _, err := migrationCanonicalRawDigest(trial.Request)
	if err != nil {
		return err
	}
	trial.SemanticDigest, _, err = migrationCanonicalRawDigest(trial.SemanticResult)
	if err != nil {
		return err
	}
	trial.ID = StableID("MHT", trial.Host, trial.Scenario, requestDigest)
	switch trial.Scenario {
	case "role-boundary":
		trial.Passed = trial.Failure != nil && trial.Failure.Code == "role-boundary-refused" &&
			strings.TrimSpace(trial.Failure.Message) != ""
	case "local-fallback":
		trial.Passed = trial.Failure != nil && trial.Failure.Code == "mcp-unavailable" &&
			strings.TrimSpace(trial.Failure.Message) != "" && trial.ExitCode == 0
	default:
		_, result, resultErr := migrationCanonicalRawDigest(trial.Result)
		trial.Passed = resultErr == nil && result != nil && trial.Failure == nil && trial.ExitCode == 0
	}
	trial.Digest = ""
	trial.Digest, err = CanonicalDigest(*trial)
	return err
}

func SealMigrationHostConformanceEvidence(
	evidence *MigrationHostConformanceEvidence,
) error {
	if evidence == nil {
		return errors.New("host conformance evidence is nil")
	}
	evidence.SchemaVersion = MigrationSchemaVersion
	evidence.Suite = migrationHostConformanceSuite
	evidence.ProtocolDigest = migrationHostProtocolDigest()
	evidence.Passed = len(evidence.Trials) > 0
	for index := range evidence.Trials {
		if err := SealMigrationHostTrial(&evidence.Trials[index]); err != nil {
			return err
		}
		evidence.Passed = evidence.Passed && evidence.Trials[index].Passed
	}
	evidence.ResultDigest = ""
	var err error
	evidence.ResultDigest, err = CanonicalDigest(*evidence)
	return err
}

func SealMigrationRetrievalGateEvidence(
	evidence *MigrationRetrievalGateEvidence,
) error {
	if evidence == nil {
		return errors.New("retrieval gate evidence is nil")
	}
	evidence.SchemaVersion = MigrationSchemaVersion
	evidence.Suite = migrationRetrievalCertificationSuite
	evidence.ResultDigest = ""
	var err error
	evidence.ResultDigest, err = CanonicalDigest(*evidence)
	return err
}

// BuildMigrationGateArtifact creates the canonical envelope for already
// sealed gate-specific evidence. RecordGate still re-reads and independently
// verifies every referenced artifact and trial before accepting the envelope.
func BuildMigrationGateArtifact(
	transactionID string,
	planDigest string,
	gate string,
	retrieval *MigrationRetrievalGateEvidence,
	hostParity *MigrationHostConformanceEvidence,
) (MigrationGateArtifact, error) {
	if transactionID == "" || !digestRE.MatchString(planDigest) {
		return MigrationGateArtifact{}, errors.New("migration gate identity is incomplete")
	}
	artifact := MigrationGateArtifact{
		SchemaVersion: MigrationSchemaVersion,
		TransactionID: transactionID,
		PlanDigest:    planDigest,
		Gate:          gate,
		Passed:        true,
		Retrieval:     retrieval,
		HostParity:    hostParity,
	}
	var suite, resultDigest string
	switch gate {
	case "retrieval-context":
		if retrieval == nil || hostParity != nil || !digestRE.MatchString(retrieval.ResultDigest) {
			return MigrationGateArtifact{}, errors.New("sealed retrieval evidence is required")
		}
		artifact.Fingerprints = map[string]string{
			"benchmark":                retrieval.Benchmark.Digest,
			"calibration":              retrieval.Calibration.Digest,
			"blinded-agent-evaluation": retrieval.BlindedEvaluation.Digest,
		}
		suite, resultDigest = migrationRetrievalCertificationSuite, retrieval.ResultDigest
	case "host-parity":
		if hostParity == nil || retrieval != nil || !digestRE.MatchString(hostParity.ResultDigest) {
			return MigrationGateArtifact{}, errors.New("sealed host conformance evidence is required")
		}
		var err error
		artifact.Fingerprints, err = migrationHostFingerprints(hostParity.Trials)
		if err != nil {
			return MigrationGateArtifact{}, err
		}
		suite, resultDigest = migrationHostConformanceSuite, hostParity.ResultDigest
	default:
		return MigrationGateArtifact{}, errors.New("typed artifact builder supports retrieval-context and host-parity only")
	}
	for _, name := range requiredMigrationGateChecks(gate) {
		artifact.Checks = append(artifact.Checks, MigrationGateCheck{
			Name: name, Passed: true, Evidence: suite + ":" + resultDigest + "#" + name,
		})
	}
	artifact.ResultDigest = ""
	var err error
	artifact.ResultDigest, err = CanonicalDigest(artifact)
	return artifact, err
}

func readBoundedMigrationEvidence(projectRoot, relative string) ([]byte, error) {
	clean := NormalizeProjectPath(relative)
	if filepath.IsAbs(relative) || clean == "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || clean != filepath.ToSlash(relative) {
		return nil, errors.New("certification evidence path must be canonical and project-relative")
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(clean))
	resolved, err := canonicalExistingPath(path)
	if err != nil {
		return nil, err
	}
	// The evidence path is fully resolved, so the root must be compared in
	// the same canonical form: an unresolved project root differs through
	// macOS /var symlinks and Windows 8.3 short names.
	root, err := canonicalExistingPath(projectRoot)
	if err != nil {
		return nil, err
	}
	if !withinRoot(root, resolved) {
		return nil, errors.New("certification evidence escapes the project")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxMigrationEvidenceBytes {
		return nil, errors.New("certification evidence must be a bounded regular non-link file")
	}
	return os.ReadFile(path)
}

func readMigrationEvidenceReference(
	projectRoot string,
	reference MigrationEvidenceArtifactReference,
) ([]byte, string, error) {
	body, err := readBoundedMigrationEvidence(projectRoot, reference.Path)
	if err != nil {
		return nil, "", err
	}
	digest := "sha256:" + SHA256Bytes(body)
	if !digestRE.MatchString(reference.Digest) || reference.Digest != digest {
		return nil, "", errors.New("certification evidence digest is stale or fabricated")
	}
	return body, digest, nil
}

// BuildRetrievalContextGateArtifact reads the four exact evidence files of the
// retrieval/context gate from the project, seals the typed evidence block, and
// derives the canonical gate artifact for the active transaction. It only
// assembles digests over real bytes; RecordGate still independently re-reads,
// replays, and verifies every nested evidence binding before a receipt exists.
func (engine *MigrationEngine) BuildRetrievalContextGateArtifact(
	benchmarkPath, calibrationPath, candidatePath, blindedPath string,
) (MigrationGateArtifact, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	reference := func(relative string) (MigrationEvidenceArtifactReference, error) {
		body, readErr := readBoundedMigrationEvidence(engine.ProjectRoot, relative)
		if readErr != nil {
			return MigrationEvidenceArtifactReference{}, fmt.Errorf("%s: %w", relative, readErr)
		}
		return MigrationEvidenceArtifactReference{
			Path: relative, Digest: "sha256:" + SHA256Bytes(body),
		}, nil
	}
	evidence := MigrationRetrievalGateEvidence{}
	if evidence.Benchmark, err = reference(benchmarkPath); err != nil {
		return MigrationGateArtifact{}, err
	}
	if evidence.Calibration, err = reference(calibrationPath); err != nil {
		return MigrationGateArtifact{}, err
	}
	if evidence.CandidateProfile, err = reference(candidatePath); err != nil {
		return MigrationGateArtifact{}, err
	}
	if evidence.BlindedEvaluation, err = reference(blindedPath); err != nil {
		return MigrationGateArtifact{}, err
	}
	if err := SealMigrationRetrievalGateEvidence(&evidence); err != nil {
		return MigrationGateArtifact{}, err
	}
	return BuildMigrationGateArtifact(
		state.TransactionID, state.PlanDigest, "retrieval-context", &evidence, nil)
}

// BuildHostParityGateArtifact wraps an already sealed host-conformance
// evidence file in the canonical gate artifact for the active transaction.
// The evidence bytes are read from the project and decoded strictly; every
// trial, parity relation, and digest is still independently re-verified by
// RecordGate before a receipt exists.
func (engine *MigrationEngine) BuildHostParityGateArtifact(
	hostConformancePath string,
) (MigrationGateArtifact, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	body, err := readBoundedMigrationEvidence(engine.ProjectRoot, hostConformancePath)
	if err != nil {
		return MigrationGateArtifact{}, fmt.Errorf("%s: %w", hostConformancePath, err)
	}
	var evidence MigrationHostConformanceEvidence
	if err := decodeStrict(body, &evidence); err != nil {
		return MigrationGateArtifact{}, fmt.Errorf("%s: %w", hostConformancePath, err)
	}
	return BuildMigrationGateArtifact(
		state.TransactionID, state.PlanDigest, "host-parity", nil, &evidence)
}

func (engine *MigrationEngine) validateGateSpecificMigrationEvidence(
	state MigrationState,
	artifact MigrationGateArtifact,
) error {
	switch artifact.Gate {
	case "retrieval-context":
		if artifact.Retrieval == nil || artifact.HostParity != nil {
			return errors.New("retrieval-context gate requires only typed retrieval evidence")
		}
		fingerprints, resultDigest, err := engine.validateMigrationRetrievalGateEvidence(
			state, artifact.PlanDigest, *artifact.Retrieval)
		if err != nil {
			return fmt.Errorf("retrieval-context evidence: %w", err)
		}
		return validateDerivedMigrationGateArtifact(artifact, fingerprints,
			migrationRetrievalCertificationSuite, resultDigest)
	case "host-parity":
		if artifact.HostParity == nil || artifact.Retrieval != nil {
			return errors.New("host-parity gate requires only typed host conformance evidence")
		}
		fingerprints, resultDigest, err := validateMigrationHostConformanceEvidence(
			state, artifact.PlanDigest, *artifact.HostParity)
		if err != nil {
			return fmt.Errorf("host-parity evidence: %w", err)
		}
		return validateDerivedMigrationGateArtifact(artifact, fingerprints,
			migrationHostConformanceSuite, resultDigest)
	default:
		if artifact.Retrieval != nil || artifact.HostParity != nil {
			return errors.New("gate-specific evidence is attached to the wrong gate")
		}
		return nil
	}
}

func validateDerivedMigrationGateArtifact(
	artifact MigrationGateArtifact,
	fingerprints map[string]string,
	suite string,
	resultDigest string,
) error {
	if !reflect.DeepEqual(artifact.Fingerprints, fingerprints) {
		return errors.New("gate fingerprints do not equal the rederived evidence fingerprints")
	}
	wantChecks := requiredMigrationGateChecks(artifact.Gate)
	if len(artifact.Checks) != len(wantChecks) {
		return errors.New("gate checks are not the exact gate-specific measured set")
	}
	seen := map[string]bool{}
	for _, check := range artifact.Checks {
		if !contains(wantChecks, check.Name) || seen[check.Name] || !check.Passed ||
			check.Evidence != suite+":"+resultDigest+"#"+check.Name {
			return errors.New("gate check is generic, duplicated, failed, or not bound to typed evidence")
		}
		seen[check.Name] = true
	}
	if !artifact.Passed {
		return errors.New("typed certification evidence does not support a passing gate")
	}
	return nil
}

func (engine *MigrationEngine) validateMigrationRetrievalGateEvidence(
	state MigrationState,
	planDigest string,
	evidence MigrationRetrievalGateEvidence,
) (map[string]string, string, error) {
	if evidence.SchemaVersion != MigrationSchemaVersion ||
		evidence.Suite != migrationRetrievalCertificationSuite {
		return nil, "", errors.New("retrieval evidence identity is invalid")
	}
	benchmarkBody, benchmarkDigest, err := readMigrationEvidenceReference(
		engine.ProjectRoot, evidence.Benchmark)
	if err != nil {
		return nil, "", fmt.Errorf("benchmark: %w", err)
	}
	var benchmark ProjectBenchmarkReport
	if err := decodeStrict(benchmarkBody, &benchmark); err != nil {
		return nil, "", fmt.Errorf("benchmark: %w", err)
	}
	environment, err := InspectMigrationCertificationEnvironment(engine.ProjectRoot)
	if err != nil {
		return nil, "", fmt.Errorf("current certification environment: %w", err)
	}
	environment.certificationAssetRoot = engine.AssetRoot
	cleanupFindingReplay, err := environment.prepareCertificationReplay()
	if err != nil {
		return nil, "", fmt.Errorf("current certification environment: %w", err)
	}
	defer cleanupFindingReplay()
	primary, err := validateFinalMigrationBenchmark(benchmark, environment, engine.ProjectRoot)
	if err != nil {
		return nil, "", fmt.Errorf("benchmark: %w", err)
	}

	calibrationBody, calibrationDigest, err := readMigrationEvidenceReference(
		engine.ProjectRoot, evidence.Calibration)
	if err != nil {
		return nil, "", fmt.Errorf("calibration: %w", err)
	}
	var calibration CalibrationReport
	if err := decodeStrict(calibrationBody, &calibration); err != nil {
		return nil, "", fmt.Errorf("calibration: %w", err)
	}
	candidateBody, _, err := readMigrationEvidenceReference(
		engine.ProjectRoot, evidence.CandidateProfile)
	if err != nil {
		return nil, "", fmt.Errorf("candidate profile: %w", err)
	}
	if err := validateFinalMigrationCalibration(
		calibration, benchmark, primary, candidateBody, environment,
	); err != nil {
		return nil, "", fmt.Errorf("calibration: %w", err)
	}

	blindedBody, blindedDigest, err := readMigrationEvidenceReference(
		engine.ProjectRoot, evidence.BlindedEvaluation)
	if err != nil {
		return nil, "", fmt.Errorf("blinded evaluation: %w", err)
	}
	var blinded MigrationBlindedAgentEvaluation
	if err := decodeStrict(blindedBody, &blinded); err != nil {
		return nil, "", fmt.Errorf("blinded evaluation: %w", err)
	}
	if err := validateMigrationBlindedAgentEvaluation(
		state, planDigest, benchmarkDigest, calibrationDigest, primary,
		environment.EvalCases, blinded,
	); err != nil {
		return nil, "", fmt.Errorf("blinded evaluation: %w", err)
	}

	expectedResult := evidence.ResultDigest
	evidence.ResultDigest = ""
	resultDigest, err := CanonicalDigest(evidence)
	if err != nil || expectedResult != resultDigest {
		return nil, "", errors.New("retrieval evidence result digest is invalid")
	}
	return map[string]string{
		"benchmark":                benchmarkDigest,
		"calibration":              calibrationDigest,
		"blinded-agent-evaluation": blindedDigest,
	}, resultDigest, nil
}

func validateFinalMigrationBenchmark(
	report ProjectBenchmarkReport,
	environment MigrationCertificationEnvironment,
	projectRoot string,
) (ProjectProfileBenchmark, error) {
	if report.SchemaVersion != 1 || report.Mode != "full" ||
		report.Suite != "project-benchmark-v1" || !report.Passed || !report.Complete ||
		!digestRE.MatchString(report.EvalFingerprint) ||
		!digestRE.MatchString(report.Generation.CorpusFingerprint) ||
		!digestRE.MatchString(report.Generation.ModelFingerprint) ||
		len(report.Profiles) == 0 || len(report.UnsupportedProfiles) != 0 {
		return ProjectProfileBenchmark{}, errors.New("final full project benchmark is incomplete or did not pass")
	}
	if err := validateMigrationBenchmarkEnvironment(report, environment); err != nil {
		return ProjectProfileBenchmark{}, err
	}
	if err := bindMigrationReplayGeneration(environment, report.Generation); err != nil {
		return ProjectProfileBenchmark{}, err
	}
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	expectedProfiles := map[string]EffectiveProfile{}
	for _, row := range baseline.Profile.EffectiveProfiles {
		expectedProfiles[row.Name] = row
	}
	requestedDigest, _ := CanonicalDigest(baseline.Profile.ProfileID)
	if report.RequestedProfile != baseline.Profile.ProfileID+"@"+requestedDigest {
		return ProjectProfileBenchmark{}, errors.New("benchmark requested profile is not the exact packaged baseline")
	}
	seenProfiles := map[string]bool{}
	var primary ProjectProfileBenchmark
	for _, profile := range report.Profiles {
		row, expected := expectedProfiles[profile.ProfileName]
		if strings.TrimSpace(profile.ProfileName) == "" || seenProfiles[profile.ProfileName] ||
			!expected || !reflect.DeepEqual(profile.ActiveLanes, row.Lanes) ||
			!reflect.DeepEqual(profile.Models, environment.ModelsByProfile[profile.ProfileName]) ||
			!strings.HasPrefix(profile.EffectiveProfile, profile.ProfileName+"@sha256:") ||
			!digestRE.MatchString(profile.ObservationDigest) || !profile.HardGatesPassed ||
			!profile.NonInferiorToLexical || !profile.ContextPackPassed ||
			len(profile.Cases) == 0 || !contains(profile.ContextPackRoles, "manager") ||
			!contains(profile.ContextPackRoles, "drafter") {
			return ProjectProfileBenchmark{}, errors.New("benchmark profile evidence is incomplete, duplicated, or failed")
		}
		seenProfiles[profile.ProfileName] = true
		for _, budget := range []string{"512", "1024", "2048", "4096"} {
			if len(profile.CasesByBudget[budget]) != len(profile.Cases) ||
				len(profile.ContextPacksByBudget[budget]) != len(profile.ContextPackCases) {
				return ProjectProfileBenchmark{}, fmt.Errorf("benchmark profile %s omits the final budget matrix", profile.ProfileName)
			}
		}
		if err := validateMigrationBenchmarkProfile(
			profile, environment, report.Generation.ID,
		); err != nil {
			return ProjectProfileBenchmark{}, fmt.Errorf(
				"benchmark profile %s: %w", profile.ProfileName, err)
		}
		observationDigest, err := MigrationProjectBenchmarkObservationDigest(report, profile)
		if err != nil || profile.ObservationDigest != observationDigest {
			return ProjectProfileBenchmark{}, fmt.Errorf("benchmark profile %s observation digest is not reproducible", profile.ProfileName)
		}
		if profile.ProfileName == migrationBaselineEffectiveProfile {
			primary = profile
		}
	}
	if len(seenProfiles) != len(expectedProfiles) {
		return ProjectProfileBenchmark{}, errors.New("benchmark omits a packaged effective-profile arm")
	}
	if primary.ProfileName == "" {
		return ProjectProfileBenchmark{}, errors.New("benchmark omits the packaged primary effective profile")
	}
	if err := validateMigrationBenchmarkNonInferiority(report); err != nil {
		return ProjectProfileBenchmark{}, err
	}
	holdout := 0
	for _, outcome := range primary.Cases {
		if outcome.Split == "holdout" {
			holdout++
		}
	}
	if holdout == 0 {
		return ProjectProfileBenchmark{}, errors.New("benchmark has no blinded holdout cases")
	}
	return primary, nil
}

// MigrationProjectBenchmarkObservationDigest rederives the stable observation
// identity used by RunProjectBenchmark. Latency is deliberately excluded in
// exactly the same way as the measurement harness.
func MigrationProjectBenchmarkObservationDigest(
	report ProjectBenchmarkReport,
	profile ProjectProfileBenchmark,
) (string, error) {
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return "", err
	}
	var row *EffectiveProfile
	for index := range baseline.Profile.EffectiveProfiles {
		if baseline.Profile.EffectiveProfiles[index].Name == profile.ProfileName {
			row = &baseline.Profile.EffectiveProfiles[index]
			break
		}
	}
	if row == nil {
		return "", errors.New("profile is not a packaged migration benchmark arm")
	}
	digestCases := append([]CaseOutcome(nil), profile.Cases...)
	for index := range digestCases {
		digestCases[index].LatencyMillis = 0
	}
	digestBudgets := map[string]QualityMetrics{}
	digestQualityBudgets := map[string]QualityMetrics{}
	for budget, metrics := range profile.MetricsByBudget {
		digestBudgets[budget] = metricsWithoutLatency(metrics)
	}
	for budget, metrics := range profile.QualityMetricsByBudget {
		digestQualityBudgets[budget] = metricsWithoutLatency(metrics)
	}
	digestBudgetCases := map[string][]CaseOutcome{}
	for budget, outcomes := range profile.CasesByBudget {
		copied := append([]CaseOutcome(nil), outcomes...)
		for index := range copied {
			copied[index].LatencyMillis = 0
		}
		digestBudgetCases[budget] = copied
	}
	return CanonicalDigest(struct {
		Suite                  string                          `json:"suite"`
		EffectiveProfile       EffectiveProfile                `json:"effectiveProfile"`
		EffectiveIdentity      string                          `json:"effectiveIdentity"`
		EvalFingerprint        string                          `json:"evalFingerprint"`
		CorpusFingerprint      string                          `json:"corpusFingerprint"`
		Runtime                RuntimeContractIdentity         `json:"runtime"`
		Models                 []ModelIdentity                 `json:"models"`
		Cases                  []CaseOutcome                   `json:"cases"`
		Metrics                QualityMetrics                  `json:"metrics"`
		MetricsBySplit         map[string]QualityMetrics       `json:"metricsBySplit"`
		MetricsByBudget        map[string]QualityMetrics       `json:"metricsByBudget"`
		QualityMetricsByBudget map[string]QualityMetrics       `json:"qualityMetricsByBudget"`
		CasesByBudget          map[string][]CaseOutcome        `json:"casesByBudget"`
		ContextPackCases       []ContextPackOutcome            `json:"contextPackCases"`
		ContextPacksByBudget   map[string][]ContextPackOutcome `json:"contextPacksByBudget"`
		ContextPackRoles       []string                        `json:"contextPackRoles"`
		FindingEvaluations     []FindingAblationReport         `json:"findingEvaluations"`
	}{
		"project-benchmark-v1", runtimeEffectiveProfile(*row), profile.EffectiveProfile,
		report.EvalFingerprint, report.Generation.CorpusFingerprint,
		RuntimeContract(report.Generation.Runtime), profile.Models, digestCases,
		metricsWithoutLatency(profile.Metrics),
		map[string]QualityMetrics{
			"development": metricsWithoutLatency(profile.MetricsBySplit["development"]),
			"holdout":     metricsWithoutLatency(profile.MetricsBySplit["holdout"]),
		},
		digestBudgets, digestQualityBudgets, digestBudgetCases,
		contextPackOutcomesForDigest(profile.ContextPackCases),
		contextPackBudgetsForDigest(profile.ContextPacksByBudget),
		profile.ContextPackRoles, profile.FindingEvaluations,
	})
}

func validateFinalMigrationCalibration(
	report CalibrationReport,
	benchmark ProjectBenchmarkReport,
	primary ProjectProfileBenchmark,
	candidateBody []byte,
	environment MigrationCertificationEnvironment,
) error {
	if report.SchemaVersion != 1 || !strings.HasPrefix(report.RunID, "calibration-") ||
		report.BaseProfile != "plugin:balanced-v1" || report.ActiveBefore == "" ||
		report.ActiveBefore != report.ActiveAfter || report.EvalDigest != benchmark.EvalFingerprint ||
		report.CorpusFingerprint != environment.CorpusFingerprint ||
		report.ModelFingerprint != environment.CalibrationModelFingerprint ||
		!reflect.DeepEqual(report.RuntimeContract, environment.RuntimeContract) ||
		!reflect.DeepEqual(
			report.FindingSuiteDigests, environment.CalibrationFindingSuiteDigests) ||
		report.Activated || report.FailureReason != "" || len(report.Candidates) == 0 ||
		len(report.ParetoFrontier) == 0 || !report.Recommended.HardGatesPassed ||
		!report.Recommended.NonInferiorToBaseline || !report.Recommended.Pareto ||
		!digestRE.MatchString(report.Recommended.BenchmarkDigest) {
		return errors.New("final calibration is stale, activated, incomplete, or failed")
	}
	if report.ActiveBefore != primary.EffectiveProfile {
		return errors.New("calibration and benchmark effective profiles disagree")
	}
	if report.CandidateDigest != "sha256:"+SHA256Bytes(candidateBody) {
		return errors.New("calibration candidate profile digest does not match exact candidate bytes")
	}
	var candidate RetrievalProfile
	if err := decodeStrict(candidateBody, &candidate); err != nil || ValidateProfile(candidate) != nil ||
		candidate.Approval != nil || candidate.BaseProfile != report.BaseProfile ||
		!strings.HasPrefix(candidate.ProfileID, "project:candidate-") {
		return errors.New("calibration candidate is invalid, approved, or not a non-activated project candidate")
	}
	return validateMigrationCalibrationRecomputation(
		report, primary, candidate, environment,
	)
}

func validateMigrationBlindedAgentEvaluation(
	state MigrationState,
	planDigest string,
	benchmarkDigest string,
	calibrationDigest string,
	primary ProjectProfileBenchmark,
	evalCases []EvalCase,
	evaluation MigrationBlindedAgentEvaluation,
) error {
	if evaluation.SchemaVersion != MigrationSchemaVersion ||
		evaluation.Suite != migrationBlindedEvaluationSuite ||
		evaluation.TransactionID != state.TransactionID || evaluation.PlanDigest != planDigest ||
		evaluation.BenchmarkDigest != benchmarkDigest || evaluation.CalibrationDigest != calibrationDigest ||
		evaluation.ProtocolDigest != migrationBlindedProtocolDigest() ||
		strings.TrimSpace(evaluation.Evaluator) == "" || !evaluation.Passed {
		return errors.New("blinded evaluation identity, protocol, binding, or result is invalid")
	}
	wantEvaluatorDigest, err := CanonicalDigest(struct {
		Evaluator string `json:"evaluator"`
		Protocol  string `json:"protocolDigest"`
	}{evaluation.Evaluator, evaluation.ProtocolDigest})
	if err != nil || evaluation.EvaluatorDigest != wantEvaluatorDigest {
		return errors.New("blinded evaluation evaluator identity is fabricated")
	}
	holdout := map[string]CaseOutcome{}
	evalByID := map[string]EvalCase{}
	for _, eval := range evalCases {
		if _, duplicate := evalByID[eval.ID]; duplicate {
			return errors.New("current evaluation corpus repeats a case ID")
		}
		evalByID[eval.ID] = eval
	}
	for _, outcome := range primary.Cases {
		if outcome.Split == "holdout" {
			eval, ok := evalByID[outcome.CaseID]
			if !ok || eval.Split != "holdout" {
				return errors.New("benchmark holdout is absent from the current evaluation corpus")
			}
			if _, duplicate := holdout[outcome.CaseID]; duplicate {
				return errors.New("benchmark repeats a holdout case ID")
			}
			holdout[outcome.CaseID] = outcome
		}
	}
	for _, eval := range evalCases {
		if eval.Split == "holdout" {
			if _, ok := holdout[eval.ID]; !ok {
				return errors.New("benchmark omits a current holdout evaluation case")
			}
		}
	}
	byCase := map[string]map[string]MigrationBlindedAgentCaseOutcome{}
	for _, outcome := range evaluation.Cases {
		benchmarkOutcome, ok := holdout[outcome.CaseID]
		if !ok || !validOne(outcome.Condition, "legacy", "compiled") {
			return errors.New("blinded evaluation contains an unknown case or condition")
		}
		if byCase[outcome.CaseID] == nil {
			byCase[outcome.CaseID] = map[string]MigrationBlindedAgentCaseOutcome{}
		}
		if _, duplicate := byCase[outcome.CaseID][outcome.Condition]; duplicate {
			return errors.New("blinded evaluation duplicates a case condition")
		}
		benchmarkOutcomeDigest, _ := CanonicalDigest(benchmarkOutcome)
		if err := validateMigrationBlindedCaseOutcome(
			outcome, benchmarkOutcomeDigest, evalByID[outcome.CaseID],
		); err != nil {
			return fmt.Errorf("case %s/%s: %w", outcome.CaseID, outcome.Condition, err)
		}
		if evaluation.Evaluator == outcome.Agent {
			return errors.New("blinded evaluation cannot be self-evaluated by the tested agent")
		}
		byCase[outcome.CaseID][outcome.Condition] = outcome
	}
	if len(byCase) != len(holdout) {
		return errors.New("blinded evaluation does not cover every benchmark holdout case")
	}
	// The sealed aggregate must equal the compiled-arm conjunction: the
	// compiled arm carries the absolute bar while legacy trials are the
	// measured baseline, and a sealed Passed that disagrees with the trials
	// it summarizes is fabricated.
	compiledTrials := 0
	expectedPassed := len(evaluation.Cases) > 0
	for _, outcome := range evaluation.Cases {
		if outcome.Condition == "compiled" {
			compiledTrials++
			expectedPassed = expectedPassed && outcome.Passed
		}
	}
	if compiledTrials == 0 || evaluation.Passed != expectedPassed {
		return errors.New("blinded evaluation aggregate result disagrees with its compiled-arm trials")
	}
	for caseID := range holdout {
		legacy, legacyOK := byCase[caseID]["legacy"]
		compiled, compiledOK := byCase[caseID]["compiled"]
		if !legacyOK || !compiledOK || legacy.QueryDigest != compiled.QueryDigest ||
			legacy.Request.Query != compiled.Request.Query ||
			compiled.FactualAccuracy < legacy.FactualAccuracy ||
			compiled.DecisionAccuracy < legacy.DecisionAccuracy ||
			compiled.Response.ContextTokens > legacy.Response.ContextTokens ||
			compiled.UnsupportedClaims != 0 || !compiled.EvidenceTracePassed ||
			compiled.Response.FullCorpusRead || len(compiled.Response.DurabilityLabels) == 0 ||
			!compiled.Passed {
			return fmt.Errorf("case %s does not demonstrate bounded non-inferior compiled context", caseID)
		}
	}
	expectedResult := evaluation.ResultDigest
	evaluation.ResultDigest = ""
	resultDigest, err := CanonicalDigest(evaluation)
	if err != nil || expectedResult != resultDigest {
		return errors.New("blinded evaluation result digest is invalid")
	}
	return nil
}

func validateMigrationBlindedCaseOutcome(
	outcome MigrationBlindedAgentCaseOutcome,
	benchmarkOutcomeDigest string,
	eval EvalCase,
) error {
	if strings.TrimSpace(outcome.Agent) == "" || strings.TrimSpace(outcome.Model) == "" ||
		outcome.Request.CaseID != outcome.CaseID || strings.TrimSpace(outcome.Request.Query) == "" ||
		!validOne(outcome.Request.Role, "manager", "drafter") || outcome.Request.TokenBudget < 128 ||
		outcome.Response.ContextTokens < 0 || outcome.Response.ContextTokens > outcome.Request.TokenBudget ||
		outcome.UnsupportedClaims < 0 || outcome.FactualAccuracy < 0 || outcome.FactualAccuracy > 1 ||
		outcome.DecisionAccuracy < 0 || outcome.DecisionAccuracy > 1 ||
		strings.TrimSpace(outcome.Response.Error) != "" {
		return errors.New("trial request, response, or score is invalid")
	}
	if outcome.Request.Query != eval.Query || outcome.Request.QueryClass != eval.QueryClass ||
		outcome.Request.Role != eval.Role || outcome.Request.TokenBudget != eval.TokenBudget ||
		!reflect.DeepEqual(outcome.Request.AllowedTiers, eval.AllowedTiers) {
		return errors.New("trial request does not exactly match its current canonical evaluation case")
	}
	agentDigest, _ := CanonicalDigest(struct {
		Agent string `json:"agent"`
		Model string `json:"model"`
	}{outcome.Agent, outcome.Model})
	queryDigest := "sha256:" + SHA256String(outcome.Request.Query)
	responseDigest, _ := CanonicalDigest(outcome.Response)
	wantID := StableID("MBE", outcome.CaseID, outcome.Condition, agentDigest, queryDigest)
	answerable := eval.Answerable == nil || *eval.Answerable
	if outcome.Answerable != answerable {
		return errors.New("trial answerability does not match its canonical evaluation case")
	}
	derivedPass := outcome.UnsupportedClaims == 0 && outcome.EvidenceTracePassed &&
		outcome.FactualAccuracy >= 0.5 && outcome.DecisionAccuracy >= 0.5 &&
		migrationBlindedAnswerShapeCorrect(outcome.Response, answerable)
	if outcome.ID != wantID || outcome.AgentDigest != agentDigest || outcome.QueryDigest != queryDigest ||
		outcome.BenchmarkOutcomeDigest != benchmarkOutcomeDigest ||
		outcome.ResponseDigest != responseDigest || outcome.Passed != derivedPass {
		return errors.New("trial identifiers, exact transcript digest, benchmark binding, or result is invalid")
	}
	expected := outcome.Digest
	outcome.Digest = ""
	digest, err := CanonicalDigest(outcome)
	if err != nil || expected != digest {
		return errors.New("trial digest is invalid")
	}
	return nil
}

func validateMigrationHostConformanceEvidence(
	state MigrationState,
	planDigest string,
	evidence MigrationHostConformanceEvidence,
) (map[string]string, string, error) {
	if evidence.SchemaVersion != MigrationSchemaVersion ||
		evidence.Suite != migrationHostConformanceSuite ||
		evidence.TransactionID != state.TransactionID || evidence.PlanDigest != planDigest ||
		evidence.ProtocolDigest != migrationHostProtocolDigest() || !evidence.Passed {
		return nil, "", errors.New("host evidence identity, protocol, binding, or result is invalid")
	}
	// Every mandatory host must prove its complete scenario matrix. Codex is
	// covered only when the project captured it, and then in full: partial
	// optional-host evidence is rejected, so the gate stays meaningful for
	// exactly the hosts the project actually uses.
	present := migrationHostsPresent(evidence.Trials)
	hosts := append([]string(nil), migrationMandatoryHosts...)
	if present["codex"] {
		hosts = append(hosts, "codex")
	}
	required := map[string][]string{}
	for _, host := range hosts {
		required[host] = migrationHostScenarios(host)
	}
	byKey := map[string]MigrationHostTrial{}
	for _, trial := range evidence.Trials {
		if !contains(required[trial.Host], trial.Scenario) {
			return nil, "", errors.New("host evidence contains an unsupported host/scenario pair")
		}
		key := trial.Host + ":" + trial.Scenario
		if _, duplicate := byKey[key]; duplicate {
			return nil, "", errors.New("host evidence duplicates a host/scenario trial")
		}
		if err := validateMigrationHostTrial(trial); err != nil {
			return nil, "", fmt.Errorf("trial %s: %w", key, err)
		}
		byKey[key] = trial
	}
	for host, scenarios := range required {
		for _, scenario := range scenarios {
			if _, ok := byKey[host+":"+scenario]; !ok {
				return nil, "", fmt.Errorf("host evidence omits %s/%s", host, scenario)
			}
		}
	}
	agentHosts := []string{"claude"}
	if present["codex"] {
		agentHosts = append(agentHosts, "codex")
	}
	for _, scenario := range []string{"status", "retrieval", "expansion", "bounded-recovery"} {
		want := byKey["mcp:"+scenario].SemanticDigest
		for _, host := range append([]string{"cli"}, agentHosts...) {
			if byKey[host+":"+scenario].SemanticDigest != want {
				return nil, "", fmt.Errorf("host semantic parity failed for %s", scenario)
			}
		}
	}
	discoveryDigest, _, err := migrationCanonicalRawDigest(mustJSONRaw(toolDefinitions()))
	if err != nil {
		return nil, "", err
	}
	for _, host := range append([]string{"mcp"}, agentHosts...) {
		if byKey[host+":discovery"].SemanticDigest != discoveryDigest {
			return nil, "", fmt.Errorf("%s discovery did not expose the current real tool schema", host)
		}
	}
	cliStatus := byKey["cli:status"].SemanticDigest
	for _, host := range agentHosts {
		if byKey[host+":local-fallback"].SemanticDigest != cliStatus {
			return nil, "", fmt.Errorf("%s local fallback did not preserve CLI status semantics", host)
		}
	}
	fingerprints, err := migrationHostFingerprints(evidence.Trials)
	if err != nil {
		return nil, "", err
	}
	expectedResult := evidence.ResultDigest
	evidence.ResultDigest = ""
	resultDigest, err := CanonicalDigest(evidence)
	if err != nil || expectedResult != resultDigest {
		return nil, "", errors.New("host conformance result digest is invalid")
	}
	return fingerprints, resultDigest, nil
}

func migrationHostFingerprints(trials []MigrationHostTrial) (map[string]string, error) {
	byHost := map[string][]MigrationHostTrial{}
	for _, trial := range trials {
		byHost[trial.Host] = append(byHost[trial.Host], trial)
	}
	fingerprints := map[string]string{}
	for _, host := range []string{"mcp", "cli", "claude", "codex"} {
		hostTrials := append([]MigrationHostTrial(nil), byHost[host]...)
		if len(hostTrials) == 0 {
			// An optional host without captures has no fingerprint at all;
			// a present-but-empty digest would falsely attest coverage.
			continue
		}
		sort.Slice(hostTrials, func(i, j int) bool {
			return hostTrials[i].Scenario < hostTrials[j].Scenario
		})
		digest, err := CanonicalDigest(hostTrials)
		if err != nil {
			return nil, err
		}
		name := host
		if host == "claude" || host == "codex" {
			name += "-host"
		}
		fingerprints[name] = digest
	}
	return fingerprints, nil
}

func validateMigrationHostTrial(trial MigrationHostTrial) error {
	wantTransport := map[string]string{
		"mcp": "mcp-stdio-jsonrpc", "cli": "cli-process-json",
		"claude": "claude-code-plugin", "codex": "codex-plugin",
	}[trial.Host]
	if trial.Scenario == "local-fallback" {
		wantTransport = trial.Host + "-cli-fallback"
	}
	if trial.Transport != wantTransport || len(trial.Request) == 0 || len(trial.SemanticResult) == 0 {
		return errors.New("actual request, transport, and semantic result are required")
	}
	requestDigest, requestValue, err := migrationCanonicalRawDigest(trial.Request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	requestObject, ok := requestValue.(map[string]any)
	if !ok || requestObject["scenario"] != trial.Scenario {
		return errors.New("captured request does not identify its exact harness scenario")
	}
	semanticDigest, semanticValue, err := migrationCanonicalRawDigest(trial.SemanticResult)
	if err != nil {
		return fmt.Errorf("semantic result: %w", err)
	}
	wantID := StableID("MHT", trial.Host, trial.Scenario, requestDigest)
	if trial.ID != wantID || trial.SemanticDigest != semanticDigest {
		return errors.New("trial name or semantic digest was not derived from captured evidence")
	}
	passed := false
	switch trial.Scenario {
	case "role-boundary":
		passed = trial.Failure != nil && trial.Failure.Code == "role-boundary-refused" &&
			strings.TrimSpace(trial.Failure.Message) != ""
	case "local-fallback":
		passed = trial.Failure != nil && trial.Failure.Code == "mcp-unavailable" &&
			strings.TrimSpace(trial.Failure.Message) != "" && trial.ExitCode == 0 && semanticValue != nil
	default:
		_, resultValue, resultErr := migrationCanonicalRawDigest(trial.Result)
		passed = resultErr == nil && resultValue != nil && trial.Failure == nil &&
			trial.ExitCode == 0 && semanticValue != nil
	}
	if trial.Passed != passed || !passed {
		return errors.New("captured request/result/failure does not demonstrate a passing trial")
	}
	expected := trial.Digest
	trial.Digest = ""
	digest, err := CanonicalDigest(trial)
	if err != nil || expected != digest {
		return errors.New("host trial digest is invalid")
	}
	return nil
}

func mustJSONRaw(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return body
}

// MigrationHostDiscoverySchema returns the exact current MCP tool-definition
// payload used by the conformance verifier. Harnesses capture the real host
// response and compare its normalized tools array to these bytes.
func MigrationHostDiscoverySchema() json.RawMessage {
	body := mustJSONRaw(toolDefinitions())
	return append(json.RawMessage(nil), body...)
}
