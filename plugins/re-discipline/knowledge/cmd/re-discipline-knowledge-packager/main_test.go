package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSupportedTargetMatrixIsCompleteAndOrdered(t *testing.T) {
	expected := []targetSpec{
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
	}
	if len(supportedTargets) != len(expected) {
		t.Fatalf("target count = %d, want %d", len(supportedTargets), len(expected))
	}
	for index := range expected {
		if supportedTargets[index] != expected[index] {
			t.Fatalf("target %d = %#v, want %#v", index, supportedTargets[index], expected[index])
		}
	}
}

func TestRuntimeBuildIDCommitsSharedRuntimeAssets(t *testing.T) {
	pluginRoot := t.TempDir()
	moduleRoot := filepath.Join(pluginRoot, "knowledge")
	directories := []string{
		"cmd/re-discipline-knowledge",
		"internal/knowledge",
	}
	directories = append(directories, sharedAssetRoots...)
	for _, relative := range directories {
		if err := os.MkdirAll(
			filepath.Join(moduleRoot, filepath.FromSlash(relative)), 0o755,
		); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		"go.mod":                              []byte("module example.test/runtime\n\ngo 1.26.5\n"),
		"go.sum":                              {},
		"THIRD_PARTY_NOTICES.md":              []byte("# Notices\n"),
		"../LICENSE":                          []byte("MIT License\n"),
		"profiles/balanced-v1.json":           []byte("{\"revision\":1}\n"),
		"cmd/re-discipline-knowledge/main.go": []byte("package main\n"),
		"internal/knowledge/runtime.go":       []byte("package knowledge\n"),
	}
	for relative, body := range files {
		path := filepath.Join(moduleRoot, filepath.FromSlash(relative))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if repeated != before {
		t.Fatalf("unchanged build inputs were nondeterministic: %s != %s", before, repeated)
	}
	if err := os.WriteFile(
		filepath.Join(moduleRoot, "profiles", "balanced-v1.json"),
		[]byte("{\"revision\":2}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	after, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("shared retrieval profile mutation did not change runtime build identity")
	}
}

func TestValidateOutputRootAcceptsOnlyModuleBin(t *testing.T) {
	moduleRoot := t.TempDir()
	expected := filepath.Join(moduleRoot, "bin")
	output, err := validateOutputRoot(moduleRoot, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(output, expected) {
		t.Fatalf("output = %s, want %s", output, expected)
	}
	for _, invalid := range []string{".", "..", "dist", filepath.Join(moduleRoot, "nested", "bin")} {
		if _, err := validateOutputRoot(moduleRoot, invalid); err == nil {
			t.Fatalf("unsafe output %q was accepted", invalid)
		}
	}
}

func TestPackagePathRejectsEscapesAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	for _, invalid := range []string{"", ".", "..", "../outside", "/outside"} {
		if runtime.GOOS == "windows" && invalid == "/outside" {
			invalid = `C:\outside`
		}
		if _, err := packagePath(root, invalid); err == nil {
			t.Fatalf("unsafe package path %q was accepted", invalid)
		}
	}
	path, err := packagePath(root, "linux-amd64/re-discipline-knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(path, filepath.Join(root, "linux-amd64", "re-discipline-knowledge")) {
		t.Fatalf("unexpected package path %s", path)
	}
}

func TestSharedAssetPathIsRestrictedToRuntimeAssetRoots(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{
		"evals/conformance/cases.json",
		"evals/conformance/finding-cases.json",
		"models/artifacts/model.bin",
		"models/manifest.json",
		"profiles/balanced-v1.json",
		"schemas/config.schema.json",
	} {
		valid, err := sharedAssetPath(root, relative)
		if err != nil {
			t.Fatal(err)
		}
		if !samePath(valid, filepath.Join(root, filepath.FromSlash(relative))) {
			t.Fatalf("unexpected shared asset path %s", valid)
		}
	}
	for _, invalid := range []string{
		"evals/conformance",
		"models",
		"../models/artifacts/model.bin",
		"models/artifacts/../../outside",
		"scripts/build_glove_artifact.py",
	} {
		if _, err := sharedAssetPath(root, invalid); err == nil {
			t.Fatalf("unsafe shared asset path %q was accepted", invalid)
		}
	}
}

func TestPackagedRuntimeAssetsCoverRequiredDataAndPinModelExactlyOnce(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	assets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"evals/conformance/cases.json":                   "benchmark-cases",
		"evals/conformance/finding-cases.json":           "benchmark-cases",
		"models/artifacts/README.md":                     "model-artifact-documentation",
		"models/artifacts/glove-6b-50d-top50k-q8-v1.bin": "shared-model-artifact",
		"models/manifest.json":                           "model-manifest",
		"models/specs/glove-6b-50d-top50k-q8-v1.json":    "model-specification",
		"models/specs/linear-reranker-v1.json":           "model-specification",
		"profiles/balanced-v1.json":                      "retrieval-profile",
		"schemas/runtime-package-manifest.schema.json":   "json-schema",
	}
	for _, asset := range assets {
		expectedKind, requiredPath := required[asset.Path]
		if requiredPath {
			if asset.Kind != expectedKind {
				t.Fatalf("asset %s kind = %s, want %s", asset.Path, asset.Kind, expectedKind)
			}
			delete(required, asset.Path)
		}
		if _, err := sharedAssetPath(moduleRoot, asset.Path); err != nil {
			t.Fatalf("manifested shared asset %s is outside allowed roots: %v", asset.Path, err)
		}
	}
	if len(required) != 0 {
		t.Fatalf("missing required shared assets: %v", required)
	}
	const expectedArtifactDigest = "sha256:fb108eef095f00bcc06a38e10d7f9671d9e6664ab79ae8a2c1cef5b31375b2ab"
	for _, asset := range assets {
		if asset.Path == "models/artifacts/glove-6b-50d-top50k-q8-v1.bin" &&
			asset.SHA256 != expectedArtifactDigest {
			t.Fatalf("artifact digest = %s, want %s", asset.SHA256, expectedArtifactDigest)
		}
	}
}

func TestSharedRuntimeAssetsRejectUnclassifiedFiles(t *testing.T) {
	for _, relative := range []string{
		"evals/conformance/fixture/secret.bin",
		"models/artifacts/notes.txt",
		"models/specs/model.tmp",
		"profiles/profile.txt",
		"schemas/schema.md",
	} {
		if _, err := sharedAssetKind(relative); err == nil {
			t.Fatalf("unclassified shared runtime asset %q was accepted", relative)
		}
	}
}

func TestFirstPartyBuiltinModelsRequireMITLicense(t *testing.T) {
	if err := verifyFirstPartyModelLicense(
		"builtin:linear-reranker-v1", "builtin", "MIT",
	); err != nil {
		t.Fatal(err)
	}
	for _, license := range []string{"", "MIT License", "all rights reserved"} {
		if err := verifyFirstPartyModelLicense(
			"builtin:linear-reranker-v1", "builtin", license,
		); err == nil {
			t.Fatalf("conflicting first-party license %q was accepted", license)
		}
	}
	if err := verifyFirstPartyModelLicense(
		"builtin:glove-6b-50d-top50k-q8-v1", "bundled-local", "PDDL-1.0",
	); err != nil {
		t.Fatal(err)
	}
}

func TestSharedAssetVerificationRejectsTamperedProfile(t *testing.T) {
	moduleRoot := t.TempDir()
	profilePath := filepath.Join(moduleRoot, "profiles", "balanced-v1.json")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"revision\":1}\n")
	if err := os.WriteFile(profilePath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	assets := []manifestFile{{
		Kind:   "retrieval-profile",
		Path:   "profiles/balanced-v1.json",
		SHA256: "sha256:" + digest,
		Size:   int64(len(original)),
		Mode:   "0644",
	}}
	if err := verifySharedAssetFiles(moduleRoot, assets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("{\"revision\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySharedAssetFiles(moduleRoot, assets); err == nil {
		t.Fatal("tampered retrieval profile passed shared-asset verification")
	}
}

func TestPOSIXLauncherDispatchesEverySupportedPOSIXTarget(t *testing.T) {
	for _, fragment := range []string{
		"Linux) platform=linux",
		"Darwin) platform=darwin",
		"x86_64|amd64) architecture=amd64",
		"arm64|aarch64) architecture=arm64",
		`exec "$runtime_path" "$@"`,
	} {
		if !contains(posixLauncher, fragment) {
			t.Fatalf("POSIX launcher lacks %q", fragment)
		}
	}
}

func TestCanonicalWindowsLauncherCopyIsByteExact(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "re-discipline-knowledge.exe")
	if err := copyCanonicalWindowsLauncher(
		moduleRoot, destination, pinnedGo, buildID,
	); err != nil {
		t.Fatal(err)
	}
	sourceBody, err := os.ReadFile(filepath.Join(moduleRoot, "bin", "re-discipline-knowledge.exe"))
	if err != nil {
		t.Fatal(err)
	}
	destinationBody, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(destinationBody, sourceBody) {
		t.Fatal("canonical Windows launcher copy was not byte-exact")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != windowsArtifactMode {
			t.Fatalf(
				"canonical Windows launcher mode = %04o, want %04o",
				info.Mode().Perm(),
				windowsArtifactMode,
			)
		}
	}
}

func TestWindowsLauncherEmbedsAndValidatesRuntimeBuildIdentity(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "sha256:" + strings.Repeat("a", 64)
	outputRoot := t.TempDir()
	destination := filepath.Join(outputRoot, "re-discipline-knowledge.exe")
	if err := buildGoBinaryWithIdentityPath(
		moduleRoot,
		destination,
		windowsLauncherTarget,
		"./cmd/re-discipline-knowledge-launcher",
		pinnedGo,
		windowsLauncherBuildIDPath,
		expected,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyWindowsLauncherBuildIdentity(
		outputRoot, pinnedGo, expected,
	); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(expected)) {
		t.Fatal("Windows architecture dispatcher omitted its release build identity")
	}
}

func TestWindowsLauncherIdentityValidationRejectsStaleOrMissing(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "sha256:" + strings.Repeat("a", 64)
	for _, testCase := range []struct {
		name    string
		buildID string
	}{
		{name: "missing"},
		{name: "stale", buildID: "sha256:" + strings.Repeat("b", 64)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outputRoot := t.TempDir()
			if err := buildGoBinaryWithIdentityPath(
				moduleRoot,
				filepath.Join(outputRoot, "re-discipline-knowledge.exe"),
				windowsLauncherTarget,
				"./cmd/re-discipline-knowledge-launcher",
				pinnedGo,
				windowsLauncherBuildIDPath,
				testCase.buildID,
			); err != nil {
				t.Fatal(err)
			}
			err := verifyWindowsLauncherBuildIdentity(
				outputRoot, pinnedGo, expected,
			)
			if err == nil || !strings.Contains(err.Error(), "omits compiled build identity") {
				t.Fatalf("%s dispatcher identity returned %v", testCase.name, err)
			}
		})
	}
}

func TestBuiltPackageCarriesWindowsLauncherIdentityIntoManifestAndChecksums(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-hosted package build is the canonical PE producer")
	}
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := t.TempDir()
	manifest, err := buildPackageTree(moduleRoot, outputRoot, pinnedGo, buildID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyPackageContents(
		moduleRoot, outputRoot, pinnedGo, buildID,
	); err != nil {
		t.Fatal(err)
	}
	launcher := manifest.Launchers[1]
	digest, err := fileSHA256(filepath.Join(outputRoot, launcher.Path))
	if err != nil {
		t.Fatal(err)
	}
	if launcher.SHA256 != "sha256:"+digest {
		t.Fatalf("dispatcher manifest digest = %s, want sha256:%s", launcher.SHA256, digest)
	}
	sums, err := os.ReadFile(filepath.Join(outputRoot, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	wantRow := []byte(digest + "  re-discipline-knowledge.exe\n")
	if !bytes.Contains(sums, wantRow) {
		t.Fatalf("SHA256SUMS omitted dispatcher row %q", wantRow)
	}
}

func TestCanonicalWindowsRuntimeCopiesAreByteExact(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range supportedTargets {
		if target.GOOS != "windows" {
			continue
		}
		relative := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH,
			targetBinaryName(target.GOOS),
		))
		destination := filepath.Join(t.TempDir(), target.GOARCH, targetBinaryName(target.GOOS))
		if err := copyCanonicalWindowsBinary(
			moduleRoot, relative, destination, target, pinnedGo, buildID,
		); err != nil {
			t.Fatal(err)
		}
		sourceBody, err := os.ReadFile(filepath.Join(moduleRoot, "bin", filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		destinationBody, err := os.ReadFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(destinationBody, sourceBody) {
			t.Fatalf("canonical Windows %s runtime copy was not byte-exact", target.GOARCH)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != windowsArtifactMode {
				t.Fatalf(
					"canonical Windows %s runtime mode = %04o, want %04o",
					target.GOARCH,
					info.Mode().Perm(),
					windowsArtifactMode,
				)
			}
		}
	}
}

func TestCanonicalWindowsLauncherCopyRequiresExistingArtifact(t *testing.T) {
	err := copyCanonicalWindowsLauncher(
		t.TempDir(),
		filepath.Join(t.TempDir(), "re-discipline-knowledge.exe"),
		"go1.26.0",
		"sha256:"+strings.Repeat("a", 64),
	)
	if err == nil || !strings.Contains(err.Error(), "generate knowledge/bin on Windows first") {
		t.Fatalf("missing canonical Windows launcher error = %v", err)
	}
}

func TestBuildEnvironmentPinsReleaseInputs(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOAMD64", "v4")
	t.Setenv("GOARM64", "v9.5")
	t.Setenv("GOENV", "host-environment")
	t.Setenv("GOEXPERIMENT", "host-value")
	t.Setenv("GOFIPS140", "latest")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "host-workspace")

	environment := buildEnvironment("linux", "amd64", "go1.26.5")
	values := map[string]string{}
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[strings.ToUpper(key)] = value
		}
	}
	expected := map[string]string{
		"CGO_ENABLED":  "0",
		"GOAMD64":      "v1",
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "-mod=readonly",
		"GOOS":         "linux",
		"GOARCH":       "amd64",
		"GOTOOLCHAIN":  "go1.26.5",
		"GOWORK":       "off",
	}
	for key, value := range expected {
		if values[key] != value {
			t.Fatalf("%s = %q, want %q", key, values[key], value)
		}
	}
}

func TestNoticeCoverageRequiresExactToolchainAndLinkedModules(t *testing.T) {
	notices := []byte(`| Component | Version | License |
|---|---:|---|
| Go standard library | go1.26.5 | BSD-3-Clause |
| ` + "`example.test/dependency`" + ` | v1.2.3 | MIT |
`)
	dependencies := [][2]string{{"example.test/dependency", "v1.2.3"}}
	if err := verifyNoticeCoverage(notices, "go1.26.5", dependencies); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoticeCoverage(notices, "go1.26.4", dependencies); err == nil {
		t.Fatal("mismatched release compiler was accepted")
	}
	if err := verifyNoticeCoverage(
		notices,
		"go1.26.5",
		append(dependencies, [2]string{"example.test/omitted", "v4.5.6"}),
	); err == nil {
		t.Fatal("omitted linked dependency was accepted")
	}
	withUnlinked := append(
		append([]byte{}, notices...),
		[]byte("| `example.test/unlinked` | v7.8.9 | MIT |\n")...,
	)
	if err := verifyNoticeCoverage(withUnlinked, "go1.26.5", dependencies); err == nil {
		t.Fatal("unlinked module notice was accepted as a linked dependency row")
	}
}

func TestReleaseNoticesReproduceFirstPartyLicense(t *testing.T) {
	license := []byte("MIT License\r\n\r\nCopyright (c) 2026 Example\r\n")
	notices := []byte("# Notices\n\nMIT License\n\nCopyright (c) 2026 Example\n")
	if err := verifyFirstPartyLicenseCoverage(notices, license); err != nil {
		t.Fatal(err)
	}
	if err := verifyFirstPartyLicenseCoverage(
		[]byte("# Notices\n\nMIT License\n"), license,
	); err == nil {
		t.Fatal("truncated first-party license was accepted")
	}
	if err := verifyFirstPartyLicenseCoverage(notices, []byte(" \r\n")); err == nil {
		t.Fatal("empty first-party license was accepted")
	}
}

func TestUnexpectedPackageDirectoriesAreRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "linux-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "linux-amd64", "re-discipline-knowledge"),
		[]byte("fixture"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{
		"linux-amd64/re-discipline-knowledge": true,
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoUnexpectedPackageFiles(root, expected); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "unexpected"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoUnexpectedPackageFiles(root, expected); err == nil {
		t.Fatal("unexpected empty package directory was accepted")
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
