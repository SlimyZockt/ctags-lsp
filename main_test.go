package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type rpcSuccessEnvelope struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id"`
	Result  InitializeResult `json:"result"`
}

func TestInitializeLSPRequest(t *testing.T) {
	tempDir := t.TempDir()

	sourcePath := filepath.Join(tempDir, "hello.go")
	source := []byte("package demo\n\ntype Greeter struct{}\n\nfunc (Greeter) Hello() {}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	config := parseFlagsForTest(t, []string{"ctags-lsp"})
	server := &Server{
		cache: FileCache{
			content: make(map[string][]string),
		},
		ctagsBin:    config.ctagsBin,
		tagfilePath: config.tagfilePath,
		languages:   config.languages,
	}

	resp := initializeServer(t, server, tempDir)

	t.Run("json rpc response", func(t *testing.T) {
		if resp.Jsonrpc != "2.0" {
			t.Fatalf("expected jsonrpc 2.0, got %q", resp.Jsonrpc)
		}
		if string(resp.ID) != "1" {
			t.Fatalf("expected id 1, got %s", resp.ID)
		}
	})

	t.Run("server info", func(t *testing.T) {
		if resp.Result.Info.Name != "ctags-lsp" {
			t.Fatalf("expected server name ctags-lsp, got %q", resp.Result.Info.Name)
		}
	})

	t.Run("text document sync", func(t *testing.T) {
		sync := resp.Result.Capabilities.TextDocumentSync
		if sync == nil {
			t.Fatal("expected text document sync capabilities")
		}
		if sync.Change != 1 {
			t.Fatalf("expected full sync, got %d", sync.Change)
		}
		if !sync.OpenClose {
			t.Fatal("expected open/close support")
		}
		if !sync.Save {
			t.Fatal("expected save support")
		}
	})

	t.Run("server state", func(t *testing.T) {
		if server.rootPath != tempDir {
			t.Fatalf("expected root path %q, got %q", tempDir, server.rootPath)
		}
		if !server.initialized {
			t.Fatal("expected server to be initialized")
		}
	})

	t.Run("tag entries", func(t *testing.T) {
		if len(server.tagEntries) == 0 {
			t.Fatal("expected tag entries from ctags scan")
		}

		path := "hello.go"
		cases := []struct {
			name   string
			symbol string
		}{
			{name: "struct", symbol: "Greeter"},
			{name: "method", symbol: "Hello"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if !hasTag(server.tagEntries, tc.symbol, path) {
					t.Fatalf("expected tag entry for %s", tc.symbol)
				}
			})
		}
	})
}

func parseLSPResponse(t *testing.T, raw string) rpcSuccessEnvelope {
	t.Helper()

	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected response with headers and body, got %q", raw)
	}

	contentLength := 0
	for _, line := range strings.Split(parts[0], "\r\n") {
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err := strconv.Atoi(strings.TrimSpace(after))
			if err != nil {
				t.Fatalf("invalid Content-Length: %v", err)
			}
			contentLength = length
			break
		}
	}
	if contentLength == 0 {
		t.Fatalf("missing Content-Length header in %q", parts[0])
	}

	body := parts[1]
	if contentLength != len(body) {
		t.Fatalf("expected Content-Length %d, got %d", contentLength, len(body))
	}

	var resp rpcSuccessEnvelope
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp
}

func initializeServer(t *testing.T, server *Server, rootPath string) rpcSuccessEnvelope {
	t.Helper()

	rootURI := "file://" + filepath.ToSlash(rootPath)
	paramsBytes, err := json.Marshal(InitializeParams{RootURI: rootURI})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	id := json.RawMessage("1")
	req := RPCRequest{
		Jsonrpc: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  paramsBytes,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	message := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	parsedReq, err := readMessage(bufio.NewReader(strings.NewReader(message)))
	if err != nil {
		t.Fatalf("read request: %v", err)
	}

	var output bytes.Buffer
	server.out = &output
	handleRequest(server, parsedReq)

	return parseLSPResponse(t, output.String())
}

func hasTag(entries []TagEntry, name, path string) bool {
	for _, entry := range entries {
		if entry.Name == name && entry.Path == path {
			return true
		}
	}
	return false
}

func parseFlagsForTest(t *testing.T, args []string) *Config {
	t.Helper()

	config, err := parseFlags(args, io.Discard)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return config
}
