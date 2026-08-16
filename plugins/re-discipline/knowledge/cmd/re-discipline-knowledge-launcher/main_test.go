package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyCompiledBuildIDIsRejected(t *testing.T) {
	original := CompiledBuildID
	CompiledBuildID = ""
	t.Cleanup(func() { CompiledBuildID = original })
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "build identity") {
		t.Fatalf("empty dispatcher identity returned %v", err)
	}
}

func TestWindowsArchitectureSelection(t *testing.T) {
	t.Run("native amd64", func(t *testing.T) {
		t.Setenv("PROCESSOR_ARCHITEW6432", "")
		t.Setenv("PROCESSOR_ARCHITECTURE", "AMD64")
		if got := windowsArchitecture(); got != "amd64" {
			t.Fatalf("architecture = %q, want amd64", got)
		}
	})
	t.Run("native arm64", func(t *testing.T) {
		t.Setenv("PROCESSOR_ARCHITEW6432", "")
		t.Setenv("PROCESSOR_ARCHITECTURE", "ARM64")
		if got := windowsArchitecture(); got != "arm64" {
			t.Fatalf("architecture = %q, want arm64", got)
		}
	})
	t.Run("arm64 host with emulated process", func(t *testing.T) {
		t.Setenv("PROCESSOR_ARCHITEW6432", "ARM64")
		t.Setenv("PROCESSOR_ARCHITECTURE", "AMD64")
		if got := windowsArchitecture(); got != "arm64" {
			t.Fatalf("architecture = %q, want arm64", got)
		}
	})
	t.Run("unknown safely falls back to amd64", func(t *testing.T) {
		t.Setenv("PROCESSOR_ARCHITEW6432", "")
		t.Setenv("PROCESSOR_ARCHITECTURE", "x86")
		if got := windowsArchitecture(); got != "amd64" {
			t.Fatalf("architecture = %q, want amd64", got)
		}
	})
}

func TestResolveRuntimePathUsesAdjacentRegularFiles(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "re-discipline-knowledge.exe")
	runtimeDir := filepath.Join(root, "windows-amd64")
	runtimePath := filepath.Join(runtimeDir, "re-discipline-knowledge.exe")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRuntimePath(launcher, "amd64")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	want, err := filepath.Abs(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("runtime path = %q, want %q", got, filepath.Clean(want))
	}
}

func TestResolveRuntimePathRejectsLinks(t *testing.T) {
	root := t.TempDir()
	realLauncher := filepath.Join(root, "launcher-real.exe")
	launcher := filepath.Join(root, "re-discipline-knowledge.exe")
	runtimeDir := filepath.Join(root, "windows-amd64")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realLauncher, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realLauncher, launcher); err != nil {
		t.Skipf("creating a symlink is unavailable: %v", err)
	}

	if _, err := resolveRuntimePath(launcher, "amd64"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("linked launcher returned %v", err)
	}
}

func TestResolveRuntimePathRejectsLinkedRuntime(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "re-discipline-knowledge.exe")
	runtimeDir := filepath.Join(root, "windows-amd64")
	realRuntime := filepath.Join(root, "runtime-real.exe")
	runtimePath := filepath.Join(runtimeDir, "re-discipline-knowledge.exe")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{launcher, realRuntime} {
		if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realRuntime, runtimePath); err != nil {
		t.Skipf("creating a symlink is unavailable: %v", err)
	}

	if _, err := resolveRuntimePath(launcher, "amd64"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("linked runtime returned %v", err)
	}
}
