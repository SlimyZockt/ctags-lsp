package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// InitializeParams represents parameters for the 'initialize' request.
type InitializeParams struct {
	RootURI string `json:"rootUri"`
}

// InitializeResult represents the result of the 'initialize' request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	Info         ServerInfo         `json:"serverInfo"`
}

// ServerCapabilities defines the capabilities of the language server.
type ServerCapabilities struct {
	TextDocumentSync        *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	CompletionProvider      *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider      bool                     `json:"definitionProvider,omitempty"`
	WorkspaceSymbolProvider bool                     `json:"workspaceSymbolProvider,omitempty"`
	DocumentSymbolProvider  bool                     `json:"documentSymbolProvider,omitempty"`
}

// ServerInfo defines the server name and version.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TextDocumentSyncOptions defines options for text document synchronization.
type TextDocumentSyncOptions struct {
	Change    int  `json:"change"`
	OpenClose bool `json:"openClose"`
	Save      bool `json:"save"`
}

// CompletionOptions defines options for the completion provider.
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// WorkspaceSymbolParams represents the parameters for the 'workspace/symbol' request.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// DocumentSymbolParams represents the parameters for the 'textDocument/documentSymbol' request.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SymbolInformation represents information about a symbol.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// DidOpenTextDocumentParams represents the 'textDocument/didOpen' notification.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocument `json:"textDocument"`
}

// TextDocument represents a text document in the editor.
type TextDocument struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams represents the parameters used in requests that require a text document and position.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DidChangeTextDocumentParams represents the 'textDocument/didChange' notification.
type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentIdentifier identifies a text document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentContentChangeEvent represents a change in the text document.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidCloseTextDocumentParams represents the 'textDocument/didClose' notification.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidSaveTextDocumentParams represents the 'textDocument/didSave' notification.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

// CompletionParams represents the 'textDocument/completion' request.
type CompletionParams struct {
	TextDocument PositionParams `json:"textDocument"`
	Position     Position       `json:"position"`
}

// Position represents a position in a text document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// PositionParams holds the URI for position-based requests.
type PositionParams struct {
	URI string `json:"uri"`
}

// CompletionItem represents a completion suggestion.
type CompletionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

// MarkupContent represents documentation content.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionList represents a list of completion items.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Location represents a location in a text document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Range represents a range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TagEntry represents a single ctags JSON entry.
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

// Server represents the language server.
type Server struct {
	tagEntries  []TagEntry
	rootPath    string
	cache       FileCache
	initialized bool
	ctagsBin    string
	tagfilePath string
	languages   string
	output      io.Writer
	mutex       sync.Mutex
}

// FileCache stores the content of opened files for quick access.
type FileCache struct {
	mutex   sync.RWMutex
	content map[string][]string
}

// handleRequest routes JSON-RPC messages to appropriate handlers.
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

// handleInitialize processes the 'initialize' request.
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
		server.rootPath = cwd
	} else {
		rootPath := params.RootURI
		if after, ok := strings.CutPrefix(rootPath, "file://"); ok {
			rootPath = filepath.FromSlash(after)
		}
		server.rootPath = rootPath
	}

	if err := server.scanWorkspace(); err != nil {
		server.sendError(req.ID, -32603, "Internal error while scanning tags", err.Error())
		return
	}

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				Change:    1,
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

// handleShutdown processes the 'shutdown' request.
func handleShutdown(server *Server, req RPCRequest) {
	server.sendResult(req.ID, nil)
}

// handleExit processes the 'exit' notification.
func handleExit(_ *Server, _ RPCRequest) {
	os.Exit(0)
}

// handleDidOpen processes the 'textDocument/didOpen' notification.
func handleDidOpen(server *Server, req RPCRequest) {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		log.Printf("Failed to normalize path for didOpen: %v", err)
		return
	}

	content := strings.Split(params.TextDocument.Text, "\n")

	server.cache.mutex.Lock()
	server.cache.content[filePath] = content
	server.cache.mutex.Unlock()
}

// handleDidChange processes the 'textDocument/didChange' notification.
func handleDidChange(server *Server, req RPCRequest) {
	var params DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		log.Printf("Failed to normalize path for didChange: %v", err)
		return
	}

	if len(params.ContentChanges) > 0 {
		content := strings.Split(params.ContentChanges[0].Text, "\n")
		server.cache.mutex.Lock()
		server.cache.content[filePath] = content
		server.cache.mutex.Unlock()
	}
}

// handleDidClose processes the 'textDocument/didClose' notification.
func handleDidClose(server *Server, req RPCRequest) {
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		log.Printf("Failed to normalize path for didClose: %v", err)
		return
	}

	server.cache.mutex.Lock()
	delete(server.cache.content, filePath)
	server.cache.mutex.Unlock()
}

// handleDidSave processes the 'textDocument/didSave' notification.
func handleDidSave(server *Server, req RPCRequest) {
	var params DidSaveTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		log.Printf("Failed to normalize path for didSave: %v", err)
		return
	}

	if err := server.scanSingleFileTag(filePath); err != nil {
		log.Printf("Error rescanning file %s: %v", filePath, err)
	}
}

// handleCompletion processes the 'textDocument/completion' request.
func handleCompletion(server *Server, req RPCRequest) {
	var params CompletionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}
	currentFileExt := filepath.Ext(filePath)

	server.cache.mutex.RLock()
	lines, ok := server.cache.content[filePath]
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

	word, err := server.getCurrentWord(filePath, params.Position)
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

			entryFilePath := filepath.Join(server.rootPath, entry.Path)
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

// handleDefinition processes the 'textDocument/definition' request.
func handleDefinition(server *Server, req RPCRequest) {
	var params TextDocumentPositionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}

	symbol, err := server.getCurrentWord(filePath, params.Position)
	if err != nil {
		server.sendResult(req.ID, nil)
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var locations []Location
	for _, entry := range server.tagEntries {
		if entry.Name == symbol {
			uri, err := relativePathToAbsoluteURI(server.rootPath, entry.Path)
			if err != nil {
				log.Printf("Failed to build URI for %s: %v", entry.Path, err)
				continue
			}

			content, err := server.cache.GetOrLoadFileContent(entry.Path)
			if err != nil {
				log.Printf("Failed to get content for file %s: %v", entry.Path, err)
				continue
			}

			symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

			location := Location{
				URI:   uri,
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

// handleWorkspaceSymbol processes the 'workspace/symbol' request.
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
		uri, err := relativePathToAbsoluteURI(server.rootPath, entry.Path)
		if err != nil {
			log.Printf("Failed to build URI for %s: %v", entry.Path, err)
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
				URI:   uri,
				Range: symbolRange,
			},
			ContainerName: entry.Scope,
		}
		symbols = append(symbols, symbol)
	}

	server.sendResult(req.ID, symbols)
}

// handleDocumentSymbol processes the 'textDocument/documentSymbol' request.
func handleDocumentSymbol(server *Server, req RPCRequest) {
	var params DocumentSymbolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	filePath, err := toRootRelativePath(server.rootPath, params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var symbols []SymbolInformation

	for _, entry := range server.tagEntries {
		if entry.Path != filePath {
			continue
		}

		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			continue
		}

		uri, err := relativePathToAbsoluteURI(server.rootPath, entry.Path)
		if err != nil {
			log.Printf("Failed to build URI for %s: %v", entry.Path, err)
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
			Location:      Location{URI: uri, Range: symbolRange},
			ContainerName: entry.Scope,
		}

		symbols = append(symbols, symbol)
	}

	server.sendResult(req.ID, symbols)
}

// toRootRelativePath converts file URIs, absolute, or relative paths to a root-relative path.
func toRootRelativePath(rootPath, raw string) (string, error) {
	if after, ok := strings.CutPrefix(raw, "file://"); ok {
		raw = filepath.FromSlash(after)
	}

	clean := filepath.Clean(raw)

	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(rootPath, clean)
		if err != nil {
			return "", fmt.Errorf("make relative: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path outside root: %q", clean)
		}
		return rel, nil
	}

	rel := filepath.ToSlash(clean)
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside root: %q", rel)
	}
	return rel, nil
}

// relativePathToAbsoluteURI builds a file URI from a root-relative path.
func relativePathToAbsoluteURI(rootPath, rel string) (string, error) {
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside root: %q", rel)
	}
	absPath := filepath.Clean(filepath.Join(rootPath, filepath.FromSlash(rel)))
	return "file://" + filepath.ToSlash(absPath), nil
}

// readFileLines reads the content of a file and returns it as a slice of lines.
func readFileLines(filePath string) ([]string, error) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(contentBytes), "\n"), nil
}

// GetOrLoadFileContent retrieves file content from cache or loads it from disk if not present.
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

// findSymbolRangeInFile searches for the symbol in the specified line and returns its range.
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

// getCurrentWord retrieves the current word at the given position in the document.
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

// isIdentifierChar checks if a rune is a valid identifier character.
func isIdentifierChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}
