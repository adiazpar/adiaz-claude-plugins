package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	packageSchemaVersion       = 1
	runtimeName                = "re-discipline-knowledge"
	runtimeVersion             = "0.8.0"
	runtimeBuildIDPath         = "github.com/adiaz/re-discipline-knowledge/internal/knowledge.CompiledBuildID"
	windowsLauncherBuildIDPath = "main.CompiledBuildID"
	windowsArtifactMode        = 0o644
)

type targetSpec struct {
	GOOS   string
	GOARCH string
}

var supportedTargets = []targetSpec{
	{GOOS: "windows", GOARCH: "amd64"},
	{GOOS: "windows", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
}

var windowsLauncherTarget = targetSpec{GOOS: "windows", GOARCH: "amd64"}

var sharedAssetRoots = []string{
	"evals/conformance",
	"models",
	"profiles",
	"schemas",
}

type packageManifest struct {
	Schema        string          `json:"$schema"`
	SchemaVersion int             `json:"schemaVersion"`
	Runtime       manifestRuntime `json:"runtime"`
	Build         manifestBuild   `json:"build"`
	Targets       []manifestFile  `json:"targets"`
	Launchers     []manifestFile  `json:"launchers"`
	SharedAssets  []manifestFile  `json:"sharedAssets"`
	Notices       manifestFile    `json:"notices"`
}

type manifestRuntime struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	BuildID string `json:"buildId"`
}

type manifestBuild struct {
	GoToolchain string   `json:"goToolchain"`
	CGOEnabled  bool     `json:"cgoEnabled"`
	Flags       []string `json:"flags"`
	Environment []string `json:"environment"`
	TargetOrder string   `json:"targetOrder"`
}

type manifestFile struct {
	Kind   string `json:"kind"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode"`
}

type packagedModelManifest struct {
	Models []struct {
		ID             string `json:"id"`
		Implementation string `json:"implementation"`
		ArtifactFile   string `json:"artifactFile"`
		ArtifactSHA256 string `json:"artifactSha256"`
		License        string `json:"license"`
	} `json:"models"`
}

func main() {
	output := flag.String("output", "bin", "runtime package output; only knowledge/bin is supported")
	verify := flag.Bool("verify", false, "verify checksums and a clean reproducible rebuild without changing output")
	flag.Parse()

	if err := run(*output, *verify); err != nil {
		fmt.Fprintln(os.Stderr, "re-discipline-knowledge-packager:", err)
		os.Exit(1)
	}
}

func run(output string, verify bool) error {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	outputRoot, err := validateOutputRoot(moduleRoot, output)
	if err != nil {
		return err
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return err
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		return err
	}

	if verify {
		return verifyPackage(moduleRoot, outputRoot, pinnedGo, buildID)
	}
	return buildAndInstall(moduleRoot, outputRoot, pinnedGo, buildID)
}

func findModuleRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return "", err
	}
	for {
		goMod := filepath.Join(current, "go.mod")
		if body, readErr := os.ReadFile(goMod); readErr == nil {
			if bytes.Contains(body, []byte("module github.com/adiaz/re-discipline-knowledge")) {
				return filepath.Clean(current), nil
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return "", readErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("could not find the re-discipline knowledge go.mod")
		}
		current = parent
	}
}

func validateOutputRoot(moduleRoot, output string) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "", errors.New("--output cannot be empty")
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(moduleRoot, output)
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(moduleRoot, "bin")
	if !samePath(output, expected) {
		return "", fmt.Errorf("--output must resolve to %s", expected)
	}
	if samePath(output, moduleRoot) || filepath.Dir(output) == output {
		return "", errors.New("refusing unsafe output path")
	}
	return filepath.Clean(output), nil
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func readPinnedGoVersion(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "go" {
			if fields[1] == "" {
				break
			}
			return "go" + fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("go.mod does not declare a Go version")
}

func computeRuntimeBuildID(moduleRoot string) (string, error) {
	paths := []string{
		filepath.Join(moduleRoot, "go.mod"),
		filepath.Join(moduleRoot, "go.sum"),
		filepath.Join(moduleRoot, "THIRD_PARTY_NOTICES.md"),
		filepath.Join(moduleRoot, "..", "LICENSE"),
	}
	for _, directory := range []string{
		filepath.Join(moduleRoot, "cmd", "re-discipline-knowledge"),
		filepath.Join(moduleRoot, "internal", "knowledge"),
	} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("build input cannot be a symlink: %s", path)
			}
			lowerName := strings.ToLower(entry.Name())
			if !entry.IsDir() &&
				strings.HasSuffix(lowerName, ".go") &&
				!strings.HasSuffix(lowerName, "_test.go") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	for _, relativeRoot := range sharedAssetRoots {
		directory := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("build input cannot be a symlink: %s", path)
			}
			if !entry.IsDir() {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("build input is not a regular file: %s", path)
				}
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		left, _ := filepath.Rel(moduleRoot, paths[i])
		right, _ := filepath.Rel(moduleRoot, paths[j])
		return filepath.ToSlash(left) < filepath.ToSlash(right)
	})

	hash := sha256.New()
	for _, path := range paths {
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("build input is not a regular file: %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), len(body))
		if _, err := hash.Write(body); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func buildAndInstall(moduleRoot, outputRoot, pinnedGo, buildID string) error {
	parent := filepath.Dir(outputRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".re-discipline-bin-staging-")
	if err != nil {
		return err
	}
	stagingExists := true
	defer func() {
		if stagingExists {
			_ = os.RemoveAll(staging)
		}
	}()

	if _, err := buildPackageTree(moduleRoot, staging, pinnedGo, buildID); err != nil {
		return err
	}
	if _, _, err := verifyPackageContents(
		moduleRoot, staging, pinnedGo, buildID,
	); err != nil {
		return fmt.Errorf("verify staged package: %w", err)
	}
	if err := installDirectoryAtomically(staging, outputRoot); err != nil {
		return err
	}
	stagingExists = false
	return nil
}

func installDirectoryAtomically(staging, outputRoot string) error {
	parent := filepath.Dir(outputRoot)
	backup, err := uniqueAbsentPath(parent, ".re-discipline-bin-backup-")
	if err != nil {
		return err
	}
	hadOutput := false
	if info, statErr := os.Lstat(outputRoot); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("output must be a real directory: %s", outputRoot)
		}
		if err := os.Rename(outputRoot, backup); err != nil {
			return fmt.Errorf("move prior package aside: %w", err)
		}
		hadOutput = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	if err := os.Rename(staging, outputRoot); err != nil {
		if hadOutput {
			_ = os.Rename(backup, outputRoot)
		}
		return fmt.Errorf("install package: %w", err)
	}
	if hadOutput {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove replaced package backup %s: %w", backup, err)
		}
	}
	return nil
}

func uniqueAbsentPath(parent, prefix string) (string, error) {
	directory, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", err
	}
	if err := os.Remove(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func buildPackageTree(moduleRoot, outputRoot, pinnedGo, buildID string) (packageManifest, error) {
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return packageManifest{}, err
	}
	if err := verifyGoToolchain(moduleRoot, pinnedGo); err != nil {
		return packageManifest{}, err
	}

	manifest := packageManifest{
		Schema:        "../schemas/runtime-package-manifest.schema.json",
		SchemaVersion: packageSchemaVersion,
		Runtime: manifestRuntime{
			Name: runtimeName, Version: runtimeVersion, BuildID: buildID,
		},
		Build: manifestBuild{
			GoToolchain: pinnedGo,
			CGOEnabled:  false,
			Flags: []string{
				"-trimpath",
				"-buildvcs=false",
				"-ldflags=-s -w -buildid= -X " + runtimeBuildIDPath + "=" + buildID,
			},
			Environment: []string{
				"CGO_ENABLED=0",
				"GOAMD64=v1",
				"GOARM64=v8.0",
				"GOENV=off",
				"GOEXPERIMENT=",
				"GOFIPS140=off",
				"GOFLAGS=-mod=readonly",
				"GOWORK=off",
			},
			TargetOrder: "windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64",
		},
	}

	goBinaries := make([]string, 0, len(supportedTargets)+1)
	for _, target := range supportedTargets {
		relative := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH,
			targetBinaryName(target.GOOS),
		))
		destination := filepath.Join(outputRoot, filepath.FromSlash(relative))
		if err := materializeRuntimeBinary(
			moduleRoot,
			relative,
			destination,
			target,
			pinnedGo,
			buildID,
		); err != nil {
			return packageManifest{}, err
		}
		mode := "0755"
		if target.GOOS == "windows" {
			mode = "0644"
		}
		artifact, err := describeFile(outputRoot, relative, "runtime", target.GOOS, target.GOARCH, mode)
		if err != nil {
			return packageManifest{}, err
		}
		manifest.Targets = append(manifest.Targets, artifact)
		goBinaries = append(goBinaries, destination)
	}

	noticesSource := filepath.Join(moduleRoot, "THIRD_PARTY_NOTICES.md")

	posixRelative := "re-discipline-knowledge"
	posixPath := filepath.Join(outputRoot, posixRelative)
	if err := os.WriteFile(posixPath, []byte(posixLauncher), 0o755); err != nil {
		return packageManifest{}, err
	}
	if err := os.Chmod(posixPath, 0o755); err != nil {
		return packageManifest{}, err
	}
	posixArtifact, err := describeFile(outputRoot, posixRelative, "posix-dispatch", "", "", "0755")
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Launchers = append(manifest.Launchers, posixArtifact)

	windowsRelative := "re-discipline-knowledge.exe"
	windowsPath := filepath.Join(outputRoot, windowsRelative)
	if err := materializeWindowsLauncher(
		moduleRoot, windowsPath, pinnedGo, buildID,
	); err != nil {
		return packageManifest{}, err
	}
	windowsArtifact, err := describeFile(
		outputRoot, windowsRelative, "windows-architecture-dispatch", "windows", "amd64", "0644",
	)
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Launchers = append(manifest.Launchers, windowsArtifact)
	goBinaries = append(goBinaries, windowsPath)
	if err := verifyThirdPartyNotices(noticesSource, goBinaries, pinnedGo); err != nil {
		return packageManifest{}, err
	}

	sharedAssets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		return packageManifest{}, err
	}
	manifest.SharedAssets = sharedAssets

	noticesRelative := "THIRD_PARTY_NOTICES.md"
	noticesDestination := filepath.Join(outputRoot, noticesRelative)
	if err := copyRegularFile(noticesSource, noticesDestination, 0o644); err != nil {
		return packageManifest{}, err
	}
	noticesArtifact, err := describeFile(outputRoot, noticesRelative, "third-party-notices", "", "", "0644")
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Notices = noticesArtifact

	manifestBody, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return packageManifest{}, err
	}
	manifestBody = append(manifestBody, '\n')
	if err := os.WriteFile(filepath.Join(outputRoot, "manifest.json"), manifestBody, 0o644); err != nil {
		return packageManifest{}, err
	}
	if err := writeSHA256Sums(moduleRoot, outputRoot, manifest); err != nil {
		return packageManifest{}, err
	}
	return manifest, nil
}

func verifyGoToolchain(moduleRoot, pinnedGo string) error {
	command := exec.Command("go", "env", "GOVERSION")
	command.Dir = moduleRoot
	command.Env = buildEnvironment("", "", pinnedGo)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("select %s: %w\n%s", pinnedGo, err, output)
	}
	actual := strings.TrimSpace(string(output))
	if actual != pinnedGo {
		return fmt.Errorf("selected Go toolchain %q, expected %q", actual, pinnedGo)
	}
	return nil
}

func buildGoBinary(
	moduleRoot string,
	destination string,
	target targetSpec,
	mainPackage string,
	pinnedGo string,
	buildID string,
) error {
	return buildGoBinaryWithIdentityPath(
		moduleRoot, destination, target, mainPackage, pinnedGo,
		runtimeBuildIDPath, buildID,
	)
}

func buildGoBinaryWithIdentityPath(
	moduleRoot string,
	destination string,
	target targetSpec,
	mainPackage string,
	pinnedGo string,
	buildIDPath string,
	buildID string,
) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	ldflags := "-s -w -buildid="
	if buildID != "" {
		if buildIDPath == "" {
			return errors.New("compiled build identity path is required")
		}
		ldflags += " -X " + buildIDPath + "=" + buildID
	}
	command := exec.Command(
		"go",
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags="+ldflags,
		"-o",
		destination,
		mainPackage,
	)
	command.Dir = moduleRoot
	command.Env = buildEnvironment(target.GOOS, target.GOARCH, pinnedGo)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s-%s %s: %w\n%s", target.GOOS, target.GOARCH, mainPackage, err, output)
	}
	if err := os.Chmod(destination, 0o755); err != nil {
		return err
	}
	if err := verifyBuiltBinary(destination, target, pinnedGo, buildID); err != nil {
		return err
	}
	return nil
}

func materializeRuntimeBinary(
	moduleRoot string,
	relative string,
	destination string,
	target targetSpec,
	pinnedGo string,
	buildID string,
) error {
	if runtime.GOOS != "windows" && target.GOOS == "windows" {
		return copyCanonicalWindowsBinary(
			moduleRoot, relative, destination, target, pinnedGo, buildID,
		)
	}
	if err := buildGoBinary(
		moduleRoot,
		destination,
		target,
		"./cmd/re-discipline-knowledge",
		pinnedGo,
		buildID,
	); err != nil {
		return err
	}
	if target.GOOS == "windows" {
		return os.Chmod(destination, windowsArtifactMode)
	}
	return nil
}

func materializeWindowsLauncher(
	moduleRoot, destination, pinnedGo, buildID string,
) error {
	if runtime.GOOS == "windows" {
		if err := buildGoBinaryWithIdentityPath(
			moduleRoot,
			destination,
			windowsLauncherTarget,
			"./cmd/re-discipline-knowledge-launcher",
			pinnedGo,
			windowsLauncherBuildIDPath,
			buildID,
		); err != nil {
			return err
		}
		return os.Chmod(destination, windowsArtifactMode)
	}
	return copyCanonicalWindowsLauncher(
		moduleRoot, destination, pinnedGo, buildID,
	)
}

func copyCanonicalWindowsLauncher(
	moduleRoot, destination, pinnedGo, expectedBuildID string,
) error {
	return copyCanonicalWindowsBinary(
		moduleRoot,
		"re-discipline-knowledge.exe",
		destination,
		windowsLauncherTarget,
		pinnedGo,
		expectedBuildID,
	)
}

func copyCanonicalWindowsBinary(
	moduleRoot string,
	relative string,
	destination string,
	target targetSpec,
	pinnedGo string,
	expectedBuildID string,
) error {
	source, err := packagePath(filepath.Join(moduleRoot, "bin"), relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := copyRegularFile(source, destination, windowsArtifactMode); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"canonical Windows artifact %s is missing; generate knowledge/bin on Windows first",
				relative,
			)
		}
		return fmt.Errorf("copy canonical Windows artifact %s: %w", relative, err)
	}
	if err := verifyBuiltBinary(destination, target, pinnedGo, expectedBuildID); err != nil {
		return fmt.Errorf("verify canonical Windows artifact %s: %w", relative, err)
	}
	return nil
}

func verifyBuiltBinary(path string, target targetSpec, pinnedGo, expectedBuildID string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Go build information from %s: %w", path, err)
	}
	if info.GoVersion != pinnedGo {
		return fmt.Errorf(
			"binary %s used Go toolchain %q, expected %q",
			path, info.GoVersion, pinnedGo,
		)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	expected := map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        target.GOOS,
		"GOARCH":      target.GOARCH,
	}
	switch target.GOARCH {
	case "amd64":
		expected["GOAMD64"] = "v1"
	case "arm64":
		expected["GOARM64"] = "v8.0"
	}
	for key, value := range expected {
		if settings[key] != value {
			return fmt.Errorf(
				"binary %s build setting %s=%q, expected %q",
				path, key, settings[key], value,
			)
		}
	}
	if expectedBuildID != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(body, []byte(expectedBuildID)) {
			return fmt.Errorf("binary %s omits compiled build identity %s", path, expectedBuildID)
		}
	}
	return nil
}

func verifyThirdPartyNotices(noticesPath string, binaryPaths []string, pinnedGo string) error {
	notices, err := os.ReadFile(noticesPath)
	if err != nil {
		return err
	}
	firstPartyLicense, err := os.ReadFile(filepath.Join(filepath.Dir(noticesPath), "..", "LICENSE"))
	if err != nil {
		return fmt.Errorf("read first-party license: %w", err)
	}
	if err := verifyFirstPartyLicenseCoverage(notices, firstPartyLicense); err != nil {
		return err
	}
	linked := map[string]string{}
	for _, binaryPath := range binaryPaths {
		info, err := buildinfo.ReadFile(binaryPath)
		if err != nil {
			return fmt.Errorf("read dependency information from %s: %w", binaryPath, err)
		}
		for _, dependency := range info.Deps {
			if dependency.Replace != nil {
				return fmt.Errorf(
					"release binary %s contains unsupported module replacement %s => %s",
					binaryPath, dependency.Path, dependency.Replace.Path,
				)
			}
			if previous, found := linked[dependency.Path]; found && previous != dependency.Version {
				return fmt.Errorf(
					"release binaries link conflicting versions of %s: %s and %s",
					dependency.Path, previous, dependency.Version,
				)
			}
			linked[dependency.Path] = dependency.Version
		}
	}
	paths := make([]string, 0, len(linked))
	for path := range linked {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	dependencies := make([][2]string, 0, len(paths))
	for _, path := range paths {
		dependencies = append(dependencies, [2]string{path, linked[path]})
	}
	if err := verifyNoticeCoverage(notices, pinnedGo, dependencies); err != nil {
		return err
	}
	return nil
}

func verifyFirstPartyLicenseCoverage(notices, firstPartyLicense []byte) error {
	normalize := func(body []byte) []byte {
		body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
		return bytes.TrimSpace(body)
	}
	license := normalize(firstPartyLicense)
	if len(license) == 0 {
		return errors.New("first-party LICENSE is empty")
	}
	if !bytes.Contains(normalize(notices), license) {
		return errors.New("release notices do not reproduce the first-party LICENSE")
	}
	return nil
}

func verifyNoticeCoverage(notices []byte, pinnedGo string, dependencies [][2]string) error {
	rows := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(notices))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) < 5 {
			continue
		}
		component := strings.Trim(strings.TrimSpace(columns[1]), "`")
		version := strings.Trim(strings.TrimSpace(columns[2]), "`")
		if component != "" && version != "" {
			if _, duplicate := rows[component]; duplicate {
				return fmt.Errorf("third-party notices repeat component %q", component)
			}
			rows[component] = version
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if rows["Go standard library"] != pinnedGo {
		return fmt.Errorf(
			"third-party notices declare Go standard library %q, expected %q",
			rows["Go standard library"], pinnedGo,
		)
	}
	linked := map[string]string{}
	for _, dependency := range dependencies {
		path, version := dependency[0], dependency[1]
		if path == "" || version == "" {
			return fmt.Errorf("linked dependency has incomplete identity %q@%q", path, version)
		}
		linked[path] = version
		if rows[path] != version {
			return fmt.Errorf(
				"third-party notices omit linked module %s@%s",
				path, version,
			)
		}
	}
	for component := range rows {
		if strings.Contains(component, "/") {
			if _, found := linked[component]; !found {
				return fmt.Errorf(
					"third-party notices list module %s that is not linked",
					component,
				)
			}
		}
	}
	return nil
}

func buildEnvironment(goos, goarch, pinnedGo string) []string {
	replacements := map[string]string{
		"CGO_ENABLED":  "0",
		"GOAMD64":      "v1",
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "-mod=readonly",
		"GOTOOLCHAIN":  pinnedGo,
		"GOWORK":       "off",
	}
	if goos != "" {
		replacements["GOOS"] = goos
	}
	if goarch != "" {
		replacements["GOARCH"] = goarch
	}
	environment := make([]string, 0, len(os.Environ())+len(replacements))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replaced := replacements[strings.ToUpper(key)]; replaced {
				continue
			}
		}
		environment = append(environment, entry)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+replacements[key])
	}
	return environment
}

func targetBinaryName(goos string) string {
	if goos == "windows" {
		return runtimeName + ".exe"
	}
	return runtimeName
}

func copyRegularFile(source, destination string, mode fs.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, mode)
}

func describeFile(outputRoot, relative, kind, goos, goarch, mode string) (manifestFile, error) {
	path, err := packagePath(outputRoot, relative)
	if err != nil {
		return manifestFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return manifestFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return manifestFile{}, fmt.Errorf("package file is not regular: %s", relative)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		return manifestFile{}, err
	}
	return manifestFile{
		Kind: kind, GOOS: goos, GOARCH: goarch, Path: filepath.ToSlash(relative),
		SHA256: "sha256:" + digest, Size: info.Size(), Mode: mode,
	}, nil
}

func discoverSharedAssets(moduleRoot string) ([]manifestFile, error) {
	assets := []manifestFile{}
	binaryAssets := map[string]string{}
	for _, relativeRoot := range sharedAssetRoots {
		assetRoot := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(assetRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("shared runtime assets cannot contain symlinks: %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("shared runtime asset is not a regular file: %s", path)
			}
			relative, err := filepath.Rel(moduleRoot, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			kind, err := sharedAssetKind(relative)
			if err != nil {
				return err
			}
			digest, err := fileSHA256(path)
			if err != nil {
				return err
			}
			assets = append(assets, manifestFile{
				Kind: kind, Path: relative, SHA256: "sha256:" + digest,
				Size: info.Size(), Mode: "0644",
			})
			if kind == "shared-model-artifact" {
				binaryAssets[relative] = digest
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	if len(binaryAssets) == 0 {
		return nil, errors.New("shared runtime assets contain no packaged model artifact")
	}
	if err := verifySharedModelPins(moduleRoot, binaryAssets); err != nil {
		return nil, err
	}
	return assets, nil
}

func sharedAssetKind(relative string) (string, error) {
	switch {
	case relative == "evals/conformance/cases.json" ||
		relative == "evals/conformance/finding-cases.json":
		return "benchmark-cases", nil
	case strings.HasPrefix(relative, "evals/conformance/fixture/") &&
		(strings.HasSuffix(strings.ToLower(relative), ".md") ||
			strings.HasSuffix(strings.ToLower(relative), ".json") ||
			strings.HasSuffix(strings.ToLower(relative), ".jsonc")):
		return "benchmark-fixture", nil
	case relative == "models/manifest.json":
		return "model-manifest", nil
	case strings.HasPrefix(relative, "models/specs/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "model-specification", nil
	case strings.HasPrefix(relative, "models/artifacts/") &&
		strings.HasSuffix(strings.ToLower(relative), ".bin"):
		return "shared-model-artifact", nil
	case relative == "models/artifacts/README.md":
		return "model-artifact-documentation", nil
	case strings.HasPrefix(relative, "profiles/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "retrieval-profile", nil
	case strings.HasPrefix(relative, "schemas/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "json-schema", nil
	default:
		return "", fmt.Errorf("unclassified shared runtime asset %q", relative)
	}
}

func verifySharedModelPins(moduleRoot string, binaryAssets map[string]string) error {
	body, err := os.ReadFile(filepath.Join(moduleRoot, "models", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest packagedModelManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("decode model manifest for packaging: %w", err)
	}
	pinned := map[string]bool{}
	for _, model := range manifest.Models {
		if err := verifyFirstPartyModelLicense(
			model.ID, model.Implementation, model.License,
		); err != nil {
			return err
		}
		if model.ArtifactFile == "" {
			if model.ArtifactSHA256 != "" {
				return fmt.Errorf("model %q has an artifact checksum without an artifact file", model.ID)
			}
			continue
		}
		if model.Implementation != "bundled-local" {
			return fmt.Errorf("model %q has a packaged artifact but is not bundled-local", model.ID)
		}
		if strings.TrimSpace(model.License) == "" {
			return fmt.Errorf("model %q has a packaged artifact without a license", model.ID)
		}
		if _, err := sharedAssetPath(moduleRoot, model.ArtifactFile); err != nil {
			return fmt.Errorf("model %q: %w", model.ID, err)
		}
		actual, found := binaryAssets[model.ArtifactFile]
		if !found {
			return fmt.Errorf("model %q references an unshipped artifact %q", model.ID, model.ArtifactFile)
		}
		if len(model.ArtifactSHA256) != 64 ||
			!strings.EqualFold(model.ArtifactSHA256, actual) {
			return fmt.Errorf("model %q artifact checksum mismatch", model.ID)
		}
		if pinned[model.ArtifactFile] {
			return fmt.Errorf("multiple model entries reference artifact %q", model.ArtifactFile)
		}
		pinned[model.ArtifactFile] = true
	}
	for path := range binaryAssets {
		if !pinned[path] {
			return fmt.Errorf("shared model artifact %q lacks a manifest entry", path)
		}
	}
	return nil
}

func verifyFirstPartyModelLicense(id, implementation, license string) error {
	if implementation == "builtin" && license != "MIT" {
		return fmt.Errorf("first-party builtin model %q must declare the MIT license", id)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeSHA256Sums(moduleRoot, outputRoot string, manifest packageManifest) error {
	paths := manifestPaths(manifest)
	paths = append(paths, "manifest.json")
	type checksumEntry struct {
		Display string
		Path    string
	}
	entries := make([]checksumEntry, 0, len(paths)+len(manifest.SharedAssets))
	for _, relative := range paths {
		path, err := packagePath(outputRoot, relative)
		if err != nil {
			return err
		}
		entries = append(entries, checksumEntry{Display: filepath.ToSlash(relative), Path: path})
	}
	for _, asset := range manifest.SharedAssets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return err
		}
		entries = append(entries, checksumEntry{
			Display: "../" + filepath.ToSlash(asset.Path),
			Path:    path,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Display < entries[j].Display })
	var body strings.Builder
	for _, entry := range entries {
		digest, err := fileSHA256(entry.Path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&body, "%s  %s\n", digest, entry.Display)
	}
	return os.WriteFile(filepath.Join(outputRoot, "SHA256SUMS"), []byte(body.String()), 0o644)
}

func manifestPaths(manifest packageManifest) []string {
	paths := make([]string, 0, len(manifest.Targets)+len(manifest.Launchers)+1)
	for _, artifact := range manifest.Targets {
		paths = append(paths, artifact.Path)
	}
	for _, artifact := range manifest.Launchers {
		paths = append(paths, artifact.Path)
	}
	paths = append(paths, manifest.Notices.Path)
	return paths
}

func packagePath(outputRoot, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid package path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package path escapes output: %q", relative)
	}
	path := filepath.Join(outputRoot, clean)
	relativeCheck, err := filepath.Rel(outputRoot, path)
	if err != nil || relativeCheck == ".." || strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package path escapes output: %q", relative)
	}
	return path, nil
}

func sharedAssetPath(moduleRoot, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid shared asset path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	path := filepath.Join(moduleRoot, clean)
	for _, relativeRoot := range sharedAssetRoots {
		allowedRoot := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		relativeCheck, err := filepath.Rel(allowedRoot, path)
		if err == nil &&
			relativeCheck != "." &&
			relativeCheck != ".." &&
			!strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
			return path, nil
		}
	}
	return "", fmt.Errorf("shared asset path is outside packaged runtime asset roots: %q", relative)
}

func verifyPackage(moduleRoot, outputRoot, pinnedGo, buildID string) error {
	_, manifestBody, err := verifyPackageContents(
		moduleRoot, outputRoot, pinnedGo, buildID,
	)
	if err != nil {
		return err
	}

	rebuildRoot, err := os.MkdirTemp(moduleRoot, ".re-discipline-reproducibility-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rebuildRoot)
	if _, err := buildPackageTree(moduleRoot, rebuildRoot, pinnedGo, buildID); err != nil {
		return err
	}
	rebuiltManifest, err := os.ReadFile(filepath.Join(rebuildRoot, "manifest.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(manifestBody, rebuiltManifest) {
		return errors.New("reproducible rebuild changed manifest.json")
	}
	if err := comparePackageTrees(outputRoot, rebuildRoot); err != nil {
		return err
	}
	return nil
}

func verifyPackageContents(
	moduleRoot, outputRoot, pinnedGo, buildID string,
) (packageManifest, []byte, error) {
	manifest, manifestBody, err := readAndValidateManifest(moduleRoot, outputRoot, pinnedGo, buildID)
	if err != nil {
		return packageManifest{}, nil, err
	}
	if err := verifyManifestFiles(moduleRoot, outputRoot, manifest); err != nil {
		return packageManifest{}, nil, err
	}
	if err := verifyWindowsLauncherBuildIdentity(
		outputRoot, pinnedGo, buildID,
	); err != nil {
		return packageManifest{}, nil, err
	}
	expectedSums, err := expectedSHA256Sums(moduleRoot, outputRoot, manifest)
	if err != nil {
		return packageManifest{}, nil, err
	}
	actualSums, err := os.ReadFile(filepath.Join(outputRoot, "SHA256SUMS"))
	if err != nil {
		return packageManifest{}, nil, err
	}
	if !bytes.Equal(actualSums, expectedSums) {
		return packageManifest{}, nil, errors.New("SHA256SUMS does not exactly match package contents")
	}
	return manifest, manifestBody, nil
}

func verifyWindowsLauncherBuildIdentity(
	outputRoot, pinnedGo, expectedBuildID string,
) error {
	if expectedBuildID == "" {
		return errors.New("windows architecture dispatcher build identity is required")
	}
	path, err := packagePath(outputRoot, "re-discipline-knowledge.exe")
	if err != nil {
		return err
	}
	if err := verifyBuiltBinary(
		path, windowsLauncherTarget, pinnedGo, expectedBuildID,
	); err != nil {
		return fmt.Errorf("verify windows architecture dispatcher: %w", err)
	}
	return nil
}

func readAndValidateManifest(
	moduleRoot, outputRoot, pinnedGo, buildID string,
) (packageManifest, []byte, error) {
	path := filepath.Join(outputRoot, "manifest.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return packageManifest{}, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return packageManifest{}, nil, fmt.Errorf("decode manifest: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return packageManifest{}, nil, errors.New("manifest contains trailing JSON")
	}
	if manifest.Schema != "../schemas/runtime-package-manifest.schema.json" ||
		manifest.SchemaVersion != packageSchemaVersion ||
		manifest.Runtime.Name != runtimeName ||
		manifest.Runtime.Version != runtimeVersion ||
		manifest.Runtime.BuildID != buildID {
		return packageManifest{}, nil, errors.New("manifest runtime identity mismatch")
	}
	if manifest.Build.GoToolchain != pinnedGo ||
		manifest.Build.CGOEnabled ||
		manifest.Build.TargetOrder !=
			"windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64" {
		return packageManifest{}, nil, errors.New("manifest build identity mismatch")
	}
	expectedFlags := []string{
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=-s -w -buildid= -X " + runtimeBuildIDPath + "=" + buildID,
	}
	if !equalStrings(manifest.Build.Flags, expectedFlags) {
		return packageManifest{}, nil, errors.New("manifest build flags mismatch")
	}
	expectedEnvironment := []string{
		"CGO_ENABLED=0",
		"GOAMD64=v1",
		"GOARM64=v8.0",
		"GOENV=off",
		"GOEXPERIMENT=",
		"GOFIPS140=off",
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
	}
	if !equalStrings(manifest.Build.Environment, expectedEnvironment) {
		return packageManifest{}, nil, errors.New("manifest build environment mismatch")
	}
	if len(manifest.Targets) != len(supportedTargets) || len(manifest.Launchers) != 2 {
		return packageManifest{}, nil, errors.New("manifest platform matrix is incomplete")
	}
	for index, target := range supportedTargets {
		artifact := manifest.Targets[index]
		expectedPath := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH, targetBinaryName(target.GOOS),
		))
		expectedMode := "0755"
		if target.GOOS == "windows" {
			expectedMode = "0644"
		}
		if artifact.Kind != "runtime" ||
			artifact.GOOS != target.GOOS ||
			artifact.GOARCH != target.GOARCH ||
			artifact.Path != expectedPath ||
			artifact.Mode != expectedMode {
			return packageManifest{}, nil, fmt.Errorf("manifest target %d is invalid", index)
		}
	}
	if manifest.Launchers[0].Kind != "posix-dispatch" ||
		manifest.Launchers[0].Path != "re-discipline-knowledge" ||
		manifest.Launchers[0].Mode != "0755" ||
		manifest.Launchers[1].Kind != "windows-architecture-dispatch" ||
		manifest.Launchers[1].Path != "re-discipline-knowledge.exe" ||
		manifest.Launchers[1].GOOS != "windows" ||
		manifest.Launchers[1].GOARCH != "amd64" ||
		manifest.Launchers[1].Mode != "0644" {
		return packageManifest{}, nil, errors.New("manifest launcher contract mismatch")
	}
	if manifest.Notices.Kind != "third-party-notices" ||
		manifest.Notices.Path != "THIRD_PARTY_NOTICES.md" ||
		manifest.Notices.Mode != "0644" {
		return packageManifest{}, nil, errors.New("manifest notices contract mismatch")
	}
	expectedAssets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		return packageManifest{}, nil, err
	}
	if len(manifest.SharedAssets) != len(expectedAssets) {
		return packageManifest{}, nil, errors.New("manifest shared runtime asset set is incomplete")
	}
	for index := range expectedAssets {
		if manifest.SharedAssets[index] != expectedAssets[index] {
			return packageManifest{}, nil, fmt.Errorf("manifest shared asset %d mismatch", index)
		}
	}
	canonical, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return packageManifest{}, nil, err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(body, canonical) {
		return packageManifest{}, nil, errors.New("manifest is not canonical JSON")
	}
	return manifest, body, nil
}

func verifyManifestFiles(moduleRoot, outputRoot string, manifest packageManifest) error {
	files := append([]manifestFile{}, manifest.Targets...)
	files = append(files, manifest.Launchers...)
	files = append(files, manifest.Notices)
	seen := map[string]bool{}
	for _, artifact := range files {
		if seen[artifact.Path] {
			return fmt.Errorf("duplicate manifest path %q", artifact.Path)
		}
		seen[artifact.Path] = true
		path, err := packagePath(outputRoot, artifact.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("manifest path is not a regular file: %s", artifact.Path)
		}
		if info.Size() != artifact.Size {
			return fmt.Errorf("size mismatch for %s", artifact.Path)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if artifact.SHA256 != "sha256:"+digest {
			return fmt.Errorf("checksum mismatch for %s", artifact.Path)
		}
		if runtime.GOOS != "windows" {
			expectedMode, err := strconv.ParseUint(artifact.Mode, 8, 32)
			if err != nil {
				return err
			}
			if info.Mode().Perm() != fs.FileMode(expectedMode) {
				return fmt.Errorf(
					"mode mismatch for %s: got %04o want %s",
					artifact.Path, info.Mode().Perm(), artifact.Mode,
				)
			}
		}
	}
	if err := verifySharedAssetFiles(moduleRoot, manifest.SharedAssets); err != nil {
		return err
	}
	return verifyNoUnexpectedPackageFiles(outputRoot, seen)
}

func verifySharedAssetFiles(moduleRoot string, assets []manifestFile) error {
	for _, asset := range assets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("shared asset is not a regular file: %s", asset.Path)
		}
		if info.Size() != asset.Size {
			return fmt.Errorf("size mismatch for shared asset %s", asset.Path)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if asset.SHA256 != "sha256:"+digest {
			return fmt.Errorf("checksum mismatch for shared asset %s", asset.Path)
		}
		if asset.Mode != "0644" {
			return fmt.Errorf("invalid shared asset mode for %s", asset.Path)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
			return fmt.Errorf(
				"mode mismatch for shared asset %s: got %04o want 0644",
				asset.Path, info.Mode().Perm(),
			)
		}
	}
	return nil
}

func verifyNoUnexpectedPackageFiles(outputRoot string, expected map[string]bool) error {
	expected["manifest.json"] = true
	expected["SHA256SUMS"] = true
	expectedDirectories := map[string]bool{".": true}
	for relative := range expected {
		directory := filepath.Dir(filepath.FromSlash(relative))
		for directory != "." {
			expectedDirectories[filepath.ToSlash(directory)] = true
			directory = filepath.Dir(directory)
		}
	}
	return filepath.WalkDir(outputRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package cannot contain symlinks: %s", path)
		}
		relative, err := filepath.Rel(outputRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if !expectedDirectories[relative] {
				return fmt.Errorf("unexpected package directory %s", relative)
			}
			return nil
		}
		if !expected[relative] {
			return fmt.Errorf("unexpected package file %s", relative)
		}
		return nil
	})
}

func expectedSHA256Sums(moduleRoot, outputRoot string, manifest packageManifest) ([]byte, error) {
	paths := append(manifestPaths(manifest), "manifest.json")
	type checksumEntry struct {
		Display string
		Path    string
	}
	entries := make([]checksumEntry, 0, len(paths)+len(manifest.SharedAssets))
	for _, relative := range paths {
		path, err := packagePath(outputRoot, relative)
		if err != nil {
			return nil, err
		}
		entries = append(entries, checksumEntry{Display: filepath.ToSlash(relative), Path: path})
	}
	for _, asset := range manifest.SharedAssets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, checksumEntry{
			Display: "../" + filepath.ToSlash(asset.Path),
			Path:    path,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Display < entries[j].Display })
	var body strings.Builder
	for _, entry := range entries {
		digest, err := fileSHA256(entry.Path)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&body, "%s  %s\n", digest, entry.Display)
	}
	return []byte(body.String()), nil
}

func comparePackageTrees(leftRoot, rightRoot string) error {
	leftFiles, err := packageFileDigests(leftRoot)
	if err != nil {
		return err
	}
	rightFiles, err := packageFileDigests(rightRoot)
	if err != nil {
		return err
	}
	if len(leftFiles) != len(rightFiles) {
		return errors.New("reproducible rebuild changed package file count")
	}
	for path, leftDigest := range leftFiles {
		rightDigest, found := rightFiles[path]
		if !found {
			return fmt.Errorf("reproducible rebuild omitted %s", path)
		}
		if leftDigest != rightDigest {
			return fmt.Errorf("reproducible rebuild changed %s", path)
		}
	}
	return nil
}

func packageFileDigests(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package cannot contain symlinks: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = digest
		return nil
	})
	return result, err
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

const posixLauncher = `#!/bin/sh
set -eu

launcher_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
kernel=$(uname -s)
machine=$(uname -m)

case "$kernel" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *)
    echo "re-discipline-knowledge: unsupported operating system: $kernel" >&2
    exit 1
    ;;
esac

case "$machine" in
  x86_64|amd64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *)
    echo "re-discipline-knowledge: unsupported architecture: $machine" >&2
    exit 1
    ;;
esac

runtime_path="$launcher_dir/$platform-$architecture/re-discipline-knowledge"
if [ ! -f "$runtime_path" ] || [ ! -x "$runtime_path" ]; then
  echo "re-discipline-knowledge: packaged runtime is missing or not executable: $runtime_path" >&2
  exit 1
fi

exec "$runtime_path" "$@"
`
