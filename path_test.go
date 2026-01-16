package main

import (
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("relative path", func(t *testing.T) {
		raw := filepath.Join("subdir", "nested", "..", "file.go")
		got, err := normalizePath(baseDir, raw)
		if err != nil {
			t.Fatalf("normalize path: %v", err)
		}

		want := filepath.Join(baseDir, "subdir", "file.go")
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		raw := filepath.Join(baseDir, "dir", "..", "file.go")
		got, err := normalizePath(baseDir, raw)
		if err != nil {
			t.Fatalf("normalize path: %v", err)
		}

		want := filepath.Join(baseDir, "file.go")
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if _, err := normalizePath(baseDir, ""); err == nil {
			t.Fatal("expected error for empty path")
		}
	})
}

func TestPathToFileURI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.go")
	got := pathToFileURI(path)
	want := "file://" + filepath.ToSlash(path)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
