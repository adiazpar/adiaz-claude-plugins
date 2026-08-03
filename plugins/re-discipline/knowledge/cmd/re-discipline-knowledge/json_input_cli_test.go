package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeCLIRequestAcceptsBOMInputFromFileAndStdin(t *testing.T) {
	type request struct {
		Value string `json:"value"`
	}
	body := []byte("{\"value\":\"PowerShell evidence\"}\n")

	t.Run("utf8-bom-file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "request.json")
		encoded := append(append([]byte(nil), utf8BOM...), body...)
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		var got request
		if err := decodeCLIRequest(path, &got); err != nil {
			t.Fatal(err)
		}
		if got.Value != "PowerShell evidence" {
			t.Fatalf("decoded value %q", got.Value)
		}
	})

	t.Run("utf16le-stdin", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		original := os.Stdin
		os.Stdin = reader
		defer func() {
			os.Stdin = original
			reader.Close()
		}()
		if _, err := writer.Write(encodeTestUTF16(body, true)); err != nil {
			writer.Close()
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		var got request
		if err := decodeCLIRequest("-", &got); err != nil {
			t.Fatal(err)
		}
		if got.Value != "PowerShell evidence" {
			t.Fatalf("decoded value %q", got.Value)
		}
	})
}

func TestDecodeCLIRequestKeepsStrictJSONChecksAfterEncodingNormalization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	body := encodeTestUTF16([]byte("{\"value\":\"ok\",\"unknown\":true}\n"), true)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var target struct {
		Value string `json:"value"`
	}
	if err := decodeCLIRequest(path, &target); err == nil {
		t.Fatal("UTF-16 normalization bypassed strict unknown-field validation")
	}
}
