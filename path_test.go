package main

import (
	"net/url"
	"path/filepath"
	"runtime"
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
	path := filepath.Join(t.TempDir(), "nested dir", "file#1.go")
	got := pathToFileURI(path)
	want := "file://" + encodePathForTest(path)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFileURIToPathPercentDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space dir", "file#1.go")
	uri := "file://" + encodePathForTest(path)
	normalizedURI, err := normalizeFileURI(uri)
	if err != nil {
		t.Fatalf("normalizeFileURI: %v", err)
	}
	got := fileURIToPath(normalizedURI)
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestNormalizeFileURICleansPath(t *testing.T) {
	baseDir := t.TempDir()
	baseURI := pathToFileURI(baseDir)
	rawURI := baseURI + "/dir%20name/../file.go"
	normalizedURI, err := normalizeFileURI(rawURI)
	if err != nil {
		t.Fatalf("normalizeFileURI: %v", err)
	}
	want := pathToFileURI(filepath.Join(baseDir, "file.go"))
	if normalizedURI != want {
		t.Fatalf("expected %q, got %q", want, normalizedURI)
	}
}

func TestNormalizeFileURIInvalidEscape(t *testing.T) {
	_, err := normalizeFileURI("file://%ZZ")
	if err == nil {
		t.Fatal("expected error for invalid escape sequence")
	}
}

func TestNormalizeFileURIEmptyPath(t *testing.T) {
	_, err := normalizeFileURI("file://")
	if err == nil {
		t.Fatal("expected error for empty file URI")
	}
}

func TestNormalizeFileURIEmptyString(t *testing.T) {
	_, err := normalizeFileURI("")
	if err == nil {
		t.Fatal("expected error for empty file URI")
	}
}

func encodePathForTest(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()[len("file://"):]
}
