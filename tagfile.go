package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type tagfileKindMap struct {
	byLanguage map[string]map[string]string
	any        map[string]string
	kindNames  map[string]bool
}

func newTagfileKindMap() *tagfileKindMap {
	return &tagfileKindMap{
		byLanguage: make(map[string]map[string]string),
		any:        make(map[string]string),
		kindNames:  make(map[string]bool),
	}
}

func (kindMap *tagfileKindMap) add(language, letter, kind string) {
	if language == "" {
		language = "default"
	}
	if _, ok := kindMap.byLanguage[language]; !ok {
		kindMap.byLanguage[language] = make(map[string]string)
	}
	kindMap.byLanguage[language][letter] = kind
	if _, ok := kindMap.any[letter]; !ok {
		kindMap.any[letter] = kind
	}
	kindMap.kindNames[kind] = true
}

func (kindMap *tagfileKindMap) resolve(language, letter string) (string, bool) {
	if language != "" {
		if byLang, ok := kindMap.byLanguage[language]; ok {
			if kind, ok := byLang[letter]; ok {
				return kind, true
			}
		}
	}
	if kind, ok := kindMap.any[letter]; ok {
		return kind, true
	}
	return "", false
}

func (kindMap *tagfileKindMap) isKindName(kind string) bool {
	return kindMap.kindNames[kind]
}

// findTagsFile checks for a tags file in a few conventional locations under `root`.
func findTagsFile(root string) (string, bool) {
	tagsLocations := []string{
		"tags",
		".tags",
		".git/tags",
	}

	for _, location := range tagsLocations {
		tagsPath := filepath.Join(root, location)
		if _, err := os.Stat(tagsPath); err == nil {
			return tagsPath, true
		}
	}

	return "", false
}

// parseTagfile reads a tags file and returns entries in the same shape as `processTagsOutput`.
func parseTagfile(tagsPath string) ([]TagEntry, error) {
	file, err := os.Open(tagsPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	kindMap := newTagfileKindMap()
	entries := make([]TagEntry, 0, 1024)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "!") {
			parseTagfileKindDescription(trimmed, kindMap)
			continue
		}

		entry, ok := parseTagfileEntry(line, tagsPath, kindMap)
		if ok {
			entries = append(entries, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// parseTagfileKindDescription records kind letter mappings from tagfile header lines.
func parseTagfileKindDescription(line string, kindMap *tagfileKindMap) {
	if !strings.HasPrefix(line, "!_TAG_KIND_DESCRIPTION") {
		return
	}

	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return
	}

	language := strings.TrimPrefix(fields[0], "!_TAG_KIND_DESCRIPTION")
	if after, ok := strings.CutPrefix(language, "!"); ok {
		language = after
	} else {
		language = ""
	}

	parts := strings.SplitN(fields[1], ",", 2)
	if len(parts) != 2 {
		return
	}

	letter := parts[0]
	kind := parts[1]
	if letter == "" || kind == "" {
		return
	}

	kindMap.add(language, letter, kind)
}

// parseTagfileEntry parses a single tags file line into a TagEntry.
// It skips invalid entries and entries whose paths can't be normalized to file URIs.
func parseTagfileEntry(line, tagsPath string, kindMap *tagfileKindMap) (TagEntry, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 3 {
		return TagEntry{}, false
	}

	entry := TagEntry{
		Type:    "tag",
		Name:    fields[0],
		Path:    fields[1],
		Pattern: strings.TrimSuffix(fields[2], ";\""),
	}

	kindField := ""
	nextFieldIndex := 3
	if len(fields) > 3 && !strings.Contains(fields[3], ":") {
		kindField = fields[3]
		nextFieldIndex = 4
	}

	for _, field := range fields[nextFieldIndex:] {
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}

		switch key {
		case "line":
			if lineNum, err := strconv.Atoi(value); err == nil {
				entry.Line = lineNum
			}
		case "language":
			entry.Language = value
		case "kind":
			kindField = value
		case "typeref":
			entry.TypeRef = value
		case "scope":
			entry.Scope = value
		case "scopeKind":
			entry.ScopeKind = value
		default:
			if entry.Scope == "" && entry.ScopeKind == "" && kindMap.isKindName(key) {
				entry.ScopeKind = key
				entry.Scope = value
			}
		}
	}

	if entry.Line == 0 {
		if lineNum, err := strconv.Atoi(entry.Pattern); err == nil {
			entry.Line = lineNum
		}
	}

	if kindField != "" {
		kindField = resolveTagfileKind(kindField, &entry, kindMap)
		entry.Kind = kindField
	}

	uri, err := tagfilePathToFileURI(tagsPath, entry.Path)
	if err != nil {
		log.Printf("Failed to normalize path for %s: %v", entry.Path, err)
		return TagEntry{}, false
	}
	entry.Path = uri

	return entry, true
}

// resolveTagfileKind maps a kind letter to its kind name using tagfile metadata.
func resolveTagfileKind(kindField string, entry *TagEntry, kindMap *tagfileKindMap) string {
	if len(kindField) != 1 {
		return kindField
	}

	if mapped, ok := kindMap.resolve(entry.Language, kindField); ok {
		return mapped
	}
	return kindField
}

// tagfilePathToFileURI normalizes a tags-file path to an absolute file URI.
// Relative paths are interpreted relative to the tagfile's directory.
func tagfilePathToFileURI(tagsPath, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}
	baseDir := filepath.Dir(tagsPath)
	normalized, err := normalizePath(baseDir, raw)
	if err != nil {
		return "", err
	}
	return pathToFileURI(normalized), nil
}
