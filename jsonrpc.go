package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

type RPCRequest struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type RPCSuccessResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result"`
}

type RPCErrorResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Error   *RPCError        `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// readMessage parses a single JSON-RPC message framed by `Content-Length` headers.
// It validates the request `id` shape (string or integer) when present.
func readMessage(reader *bufio.Reader) (RPCRequest, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\r')
		if err != nil {
			return RPCRequest{}, fmt.Errorf("error reading header: %w", err)
		}
		b, err := reader.ReadByte()
		if err != nil {
			return RPCRequest{}, fmt.Errorf("error reading header: %w", err)
		}
		if b != '\n' {
			return RPCRequest{}, fmt.Errorf("line endings must be \\r\\n")
		}
		if line == "\r" {
			break
		}
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			clStr := strings.TrimSpace(after)
			cl, err := strconv.Atoi(clStr)
			if err != nil {
				return RPCRequest{}, fmt.Errorf("invalid Content-Length: %v", err)
			}
			contentLength = cl
		}
	}

	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return RPCRequest{}, fmt.Errorf("error reading body: %w", err)
	}

	var req RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return RPCRequest{}, fmt.Errorf("invalid JSON-RPC request: %v", err)
	}
	if isInvalidID(req.ID) {
		return RPCRequest{}, fmt.Errorf("id must be a string or integer")
	}

	return req, nil
}

func isInvalidID(id *json.RawMessage) bool {
	if id == nil {
		return false
	}

	var s string
	if json.Unmarshal(*id, &s) == nil {
		return false
	}

	var n int64
	if json.Unmarshal(*id, &n) == nil {
		return false
	}

	return true
}

func isNotification(req RPCRequest) bool {
	return req.ID == nil
}

func (server *Server) sendResult(id *json.RawMessage, result any) {
	response := RPCSuccessResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Result:  result,
	}
	server.sendResponse(response)
}

func (server *Server) sendError(id *json.RawMessage, code int, message string, data any) {
	response := RPCErrorResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	server.sendResponse(response)
}

// sendResponse writes a JSON-RPC response to `server.output`.
func (server *Server) sendResponse(resp any) {
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		return
	}

	fmt.Fprintf(server.output, "Content-Length: %d\r\n\r\n%s", len(body), string(body))
}
