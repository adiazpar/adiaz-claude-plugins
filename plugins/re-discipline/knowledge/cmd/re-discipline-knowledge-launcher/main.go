package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CompiledBuildID is injected by the release packager. Keeping the dispatcher
// bound to the same source identity as the runtime targets prevents a stale
// top-level launcher from being checksummed into an otherwise current package.
var CompiledBuildID = "development"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "re-discipline-knowledge launcher:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if strings.TrimSpace(CompiledBuildID) == "" {
		return errors.New("compiled build identity is empty")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate launcher: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve launcher: %w", err)
	}

	arch := windowsArchitecture()
	runtimePath := filepath.Join(
		filepath.Dir(executable),
		"windows-"+arch,
		"re-discipline-knowledge.exe",
	)
	info, err := os.Stat(runtimePath)
	if err != nil {
		return fmt.Errorf("locate windows-%s runtime: %w", arch, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime is not a regular file: %s", runtimePath)
	}

	command := exec.Command(runtimePath, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()
	err = command.Run()
	if err == nil {
		return nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	}
	return fmt.Errorf("start runtime: %w", err)
}

func windowsArchitecture() string {
	native := strings.ToLower(strings.TrimSpace(os.Getenv("PROCESSOR_ARCHITEW6432")))
	process := strings.ToLower(strings.TrimSpace(os.Getenv("PROCESSOR_ARCHITECTURE")))
	if native == "arm64" || process == "arm64" {
		return "arm64"
	}
	return "amd64"
}
