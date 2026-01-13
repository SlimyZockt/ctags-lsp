package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

// RPCRequest represents a JSON-RPC request structure.
type RPCRequest struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// RPCSuccessResponse represents a successful JSON-RPC response structure.
type RPCSuccessResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result"`
}

// RPCErrorResponse represents an error JSON-RPC response structure.
type RPCErrorResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Error   *RPCError        `json:"error"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// readMessage reads a single JSON-RPC message from the reader.
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

// sendResult sends a successful JSON-RPC response.
func (server *Server) sendResult(id *json.RawMessage, result any) {
	response := RPCSuccessResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Result:  result,
	}
	server.sendResponse(response)
}

// sendError sends an error JSON-RPC response.
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

// sendResponse marshals and sends the JSON-RPC response with appropriate headers.
func (server *Server) sendResponse(resp any) {
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		return
	}

	writer := server.output
	if writer == nil {
		writer = os.Stdout
	}
	fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(body), string(body))
}
