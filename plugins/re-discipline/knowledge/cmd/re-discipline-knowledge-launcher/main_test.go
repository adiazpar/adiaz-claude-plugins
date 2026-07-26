package main

import "testing"

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
