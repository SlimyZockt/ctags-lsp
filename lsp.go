package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type InitializeParams struct {
	RootURI string `json:"rootUri"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	Info         ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	TextDocumentSync        *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	CompletionProvider      *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider      bool                     `json:"definitionProvider,omitempty"`
	WorkspaceSymbolProvider bool                     `json:"workspaceSymbolProvider,omitempty"`
	DocumentSymbolProvider  bool                     `json:"documentSymbolProvider,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TextDocumentSyncOptions struct {
	Change    int  `json:"change"`
	OpenClose bool `json:"openClose"`
	Save      bool `json:"save"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocument `json:"textDocument"`
}

type TextDocument struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

type CompletionParams struct {
	TextDocument PositionParams `json:"textDocument"`
	Position     Position       `json:"position"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type PositionParams struct {
	URI string `json:"uri"`
}

type CompletionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TagEntry matches the JSON entry shape produced by Universal Ctags `--output-format=json`.
// Paths are normalized to absolute file:// URIs once ingested.
type TagEntry struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Pattern   string `json:"pattern"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	Scope     string `json:"scope,omitempty"`
	ScopeKind string `json:"scopeKind,omitempty"`
	TypeRef   string `json:"typeref,omitempty"`
	Language  string `json:"language,omitempty"`
}

type Server struct {
	tagEntries  []TagEntry
	rootURI     string
	cache       FileCache
	initialized bool
	ctagsBin    string
	tagfilePath string
	languages   string
	output      io.Writer
	mutex       sync.Mutex
}

type FileCache struct {
	mutex   sync.RWMutex
	content map[string][]string
}

func handleRequest(server *Server, req RPCRequest) {
	if !server.initialized && req.Method != "initialize" && req.Method != "shutdown" && req.Method != "exit" {
		if isNotification(req) {
			return
		}
		server.sendError(req.ID, -32002, "Server not initialized", "Received request before successful initialization")
		return
	}

	switch req.Method {
	case "initialize":
		handleInitialize(server, req)
	case "initialized":
	case "shutdown":
		handleShutdown(server, req)
	case "exit":
		handleExit(server, req)
	case "textDocument/didOpen":
		handleDidOpen(server, req)
	case "textDocument/didChange":
		handleDidChange(server, req)
	case "textDocument/didClose":
		handleDidClose(server, req)
	case "textDocument/didSave":
		handleDidSave(server, req)
	case "textDocument/completion":
		handleCompletion(server, req)
	case "textDocument/definition":
		handleDefinition(server, req)
	case "workspace/symbol":
		handleWorkspaceSymbol(server, req)
	case "textDocument/documentSymbol":
		handleDocumentSymbol(server, req)
	case "$/cancelRequest":
	case "$/setTrace":
	case "$/logTrace":
	default:
		if isNotification(req) {
			return
		}
		message := fmt.Sprintf("Method not found: %s", req.Method)
		server.sendError(req.ID, -32601, message, nil)
	}
}

func handleInitialize(server *Server, req RPCRequest) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	if params.RootURI == "" {
		cwd, err := os.Getwd()
		if err != nil {
			server.sendError(req.ID, -32603, "Failed to get current working directory", err.Error())
			return
		}
		rootURI := pathToFileURI(cwd)
		server.rootURI = rootURI
	} else {
		normalizedRootURI, err := normalizeFileURI(params.RootURI)
		if err != nil {
			server.sendError(req.ID, -32602, "Invalid params", err.Error())
			return
		}
		server.rootURI = normalizedRootURI
	}

	if err := server.scanWorkspace(); err != nil {
		server.sendError(req.ID, -32603, "Internal error while scanning tags", err.Error())
		return
	}

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				Change:    1, // LSP TextDocumentSyncKindFull.
				OpenClose: true,
				Save:      true,
			},
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{".", "\""},
			},
			WorkspaceSymbolProvider: true,
			DefinitionProvider:      true,
			DocumentSymbolProvider:  true,
		},
		Info: ServerInfo{
			Name:    "ctags-lsp",
			Version: version,
		},
	}

	server.sendResult(req.ID, result)
	server.initialized = true
}

func handleShutdown(server *Server, req RPCRequest) {
	server.sendResult(req.ID, nil)
}

func handleExit(_ *Server, _ RPCRequest) {
	os.Exit(0)
}

func handleDidOpen(server *Server, req RPCRequest) {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	content := strings.Split(params.TextDocument.Text, "\n")

	server.cache.mutex.Lock()
	server.cache.content[normalizedURI] = content
	server.cache.mutex.Unlock()
}

func handleDidChange(server *Server, req RPCRequest) {
	var params DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	if len(params.ContentChanges) > 0 {
		content := strings.Split(params.ContentChanges[0].Text, "\n")
		server.cache.mutex.Lock()
		server.cache.content[normalizedURI] = content
		server.cache.mutex.Unlock()
	}
}

func handleDidClose(server *Server, req RPCRequest) {
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	server.cache.mutex.Lock()
	delete(server.cache.content, normalizedURI)
	server.cache.mutex.Unlock()
}

func handleDidSave(server *Server, req RPCRequest) {
	var params DidSaveTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	if err := server.scanSingleFileTag(normalizedURI); err != nil {
		log.Printf("Error rescanning file %s: %v", normalizedURI, err)
	}
}

func handleCompletion(server *Server, req RPCRequest) {
	var params CompletionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}
	filePath := fileURIToPath(normalizedURI)
	currentFileExt := filepath.Ext(filePath)

	server.cache.mutex.RLock()
	lines, ok := server.cache.content[normalizedURI]
	server.cache.mutex.RUnlock()

	if !ok || params.Position.Line >= len(lines) {
		server.sendError(req.ID, -32603, "Internal error", "Line out of range")
		return
	}

	lineContent := lines[params.Position.Line]
	runes := []rune(lineContent)
	isAfterDot := false
	if params.Position.Character > 0 && params.Position.Character-1 < len(runes) {
		prevChar := runes[params.Position.Character-1]
		isAfterDot = prevChar == '.'
	}

	word, err := server.getCurrentWord(normalizedURI, params.Position)
	if err != nil {
		if isAfterDot {
			word = ""
		} else {
			server.sendResult(req.ID, CompletionList{
				IsIncomplete: false,
				Items:        []CompletionItem{},
			})
			return
		}
	}

	var items []CompletionItem
	seenItems := make(map[string]bool)

	for _, entry := range server.tagEntries {
		if strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(word)) {
			if seenItems[entry.Name] {
				continue
			}

			kind := GetLSPCompletionKind(entry.Kind)

			entryFilePath := fileURIToPath(entry.Path)
			entryFileExt := filepath.Ext(entryFilePath)

			includeEntry := false

			if isAfterDot {
				if (kind == CompletionItemKindMethod || kind == CompletionItemKindFunction) && entryFileExt == currentFileExt {
					includeEntry = true
				}
			} else {
				if kind == CompletionItemKindText {
					includeEntry = true
				} else if entryFileExt == currentFileExt {
					includeEntry = true
				}
			}

			if includeEntry {
				seenItems[entry.Name] = true
				items = append(items, CompletionItem{
					Label:  entry.Name,
					Kind:   kind,
					Detail: fmt.Sprintf("%s:%d (%s)", entry.Path, entry.Line, entry.Kind),
					Documentation: &MarkupContent{
						Kind:  "plaintext",
						Value: entry.Pattern,
					},
				})
			}
		}
	}

	result := CompletionList{
		IsIncomplete: false,
		Items:        items,
	}

	server.sendResult(req.ID, result)
}

func handleDefinition(server *Server, req RPCRequest) {
	var params TextDocumentPositionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	symbol, err := server.getCurrentWord(normalizedURI, params.Position)
	if err != nil {
		server.sendResult(req.ID, nil)
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var locations []Location
	for _, entry := range server.tagEntries {
		if entry.Name == symbol {
			content, err := server.cache.GetOrLoadFileContent(entry.Path)
			if err != nil {
				log.Printf("Failed to get content for file %s: %v", entry.Path, err)
				continue
			}

			symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

			location := Location{
				URI:   entry.Path,
				Range: symbolRange,
			}
			locations = append(locations, location)
		}
	}

	if len(locations) == 0 {
		server.sendResult(req.ID, nil)
	} else if len(locations) == 1 {
		server.sendResult(req.ID, locations[0])
	} else {
		server.sendResult(req.ID, locations)
	}
}

func handleWorkspaceSymbol(server *Server, req RPCRequest) {
	var params WorkspaceSymbolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	query := params.Query
	var symbols []SymbolInformation

	server.mutex.Lock()
	defer server.mutex.Unlock()

	for _, entry := range server.tagEntries {
		if query != "" && entry.Name != query {
			continue
		}

		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			continue
		}
		content, err := server.cache.GetOrLoadFileContent(entry.Path)
		if err != nil {
			log.Printf("Failed to get content for file %s: %v", entry.Path, err)
			continue
		}

		symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

		symbol := SymbolInformation{
			Name: entry.Name,
			Kind: kind,
			Location: Location{
				URI:   entry.Path,
				Range: symbolRange,
			},
			ContainerName: entry.Scope,
		}
		symbols = append(symbols, symbol)
	}

	server.sendResult(req.ID, symbols)
}

func handleDocumentSymbol(server *Server, req RPCRequest) {
	var params DocumentSymbolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var symbols []SymbolInformation

	for _, entry := range server.tagEntries {
		if entry.Path != normalizedURI {
			continue
		}

		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			continue
		}

		content, err := server.cache.GetOrLoadFileContent(entry.Path)
		if err != nil {
			log.Printf("Failed to get content for file %s: %v", entry.Path, err)
			continue
		}

		symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

		symbol := SymbolInformation{
			Name:          entry.Name,
			Kind:          kind,
			Location:      Location{URI: entry.Path, Range: symbolRange},
			ContainerName: entry.Scope,
		}

		symbols = append(symbols, symbol)
	}

	server.sendResult(req.ID, symbols)
}

// normalizeFileURI expects external URIs.
func normalizeFileURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		// Surface parsing failures so we never normalize malformed URIs.
		return "", fmt.Errorf("failed to parse URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		// The server only supports file:// URIs for filesystem-backed documents.
		return "", fmt.Errorf("expected file:// URI: %q", uri)
	}
	if parsed.Path == "" {
		// Empty paths cannot be resolved to a filesystem location.
		return "", fmt.Errorf("empty file URI")
	}

	path := filepath.Clean(filepath.FromSlash(parsed.Path))

	absPath, err := filepath.Abs(path)
	if err != nil {
		// Avoid emitting a bogus URI if the filesystem path cannot be resolved.
		return "", fmt.Errorf("failed to resolve path %q: %w", path, err)
	}

	return pathToFileURI(absPath), nil
}

// fileURIToPath expects normalized URIs.
func fileURIToPath(uri string) string {
	parsed, _ := url.Parse(uri)
	return filepath.Clean(filepath.FromSlash(parsed.Path))
}

// pathToFileURI expects an absolute, cleaned filesystem path.
func pathToFileURI(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		slashPath = "/" + slashPath // Turns invalid "file://C:/" into valid "file:///C:/"
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

// normalizePath expects raw filesystem paths from ctags/tagfiles, not file:// URIs.
func normalizePath(baseDir, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}

	clean := filepath.Clean(raw)
	if !filepath.IsAbs(clean) {
		clean = filepath.Clean(filepath.Join(baseDir, clean))
	}
	return clean, nil
}

func readFileLines(fileURI string) ([]string, error) {
	filePath := fileURIToPath(fileURI)
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(contentBytes), "\n"), nil
}

func (cache *FileCache) GetOrLoadFileContent(filePath string) ([]string, error) {
	cache.mutex.RLock()
	content, ok := cache.content[filePath]
	cache.mutex.RUnlock()
	if ok {
		return content, nil
	}
	lines, err := readFileLines(filePath)
	if err != nil {
		return nil, err
	}
	cache.mutex.Lock()
	cache.content[filePath] = lines
	cache.mutex.Unlock()
	return lines, nil
}

// findSymbolRangeInFile returns a range for `symbolName` on `lineNumber` (1-based).
func findSymbolRangeInFile(lines []string, symbolName string, lineNumber int) Range {
	lineIdx := lineNumber - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: 0},
		}
	}

	lineContent := lines[lineIdx]
	startChar := strings.Index(lineContent, symbolName)
	if startChar == -1 {
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: len([]rune(lineContent))},
		}
	}

	endChar := startChar + len([]rune(symbolName))

	return Range{
		Start: Position{Line: lineIdx, Character: startChar},
		End:   Position{Line: lineIdx, Character: endChar},
	}
}

func (server *Server) getCurrentWord(filePath string, pos Position) (string, error) {
	lines, err := server.cache.GetOrLoadFileContent(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to load file content: %v", err)
	}

	if pos.Line >= len(lines) {
		return "", fmt.Errorf("line %d out of range", pos.Line)
	}

	line := lines[pos.Line]
	runes := []rune(line)
	if pos.Character > len(runes) {
		return "", fmt.Errorf("character %d out of range", pos.Character)
	}

	start := pos.Character
	for start > 0 && isIdentifierChar(runes[start-1]) {
		start--
	}

	end := pos.Character
	for end < len(runes) && isIdentifierChar(runes[end]) {
		end++
	}

	if start == end {
		return "", fmt.Errorf("no word found at position")
	}

	return string(runes[start:end]), nil
}

func isIdentifierChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}
