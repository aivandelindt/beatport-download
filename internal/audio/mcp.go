package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// MCPClient talks to audio-analyzer-rs mcp-server over stdio NDJSON JSON-RPC.
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	id     atomic.Int64
	cancel context.CancelFunc
	mu     sync.Mutex
}

type mcpRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type mcpToolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// StartMCP launches mcp-server and completes the initialize handshake.
func StartMCP(ctx context.Context, binPath string) (*MCPClient, error) {
	if binPath == "" {
		return nil, fmt.Errorf("mcp-server path empty")
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, binPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = &logWriter{prefix: "audio-analyzer-mcp"}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	c := &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		cancel: cancel,
	}
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *MCPClient) initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "beatportdl-ui",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if err := c.notify("notifications/initialized", map[string]interface{}{}); err != nil {
		return fmt.Errorf("mcp initialized notify: %w", err)
	}
	return nil
}

// CallTool invokes an MCP tool and returns the text content.
func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (string, error) {
	raw, err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return "", err
	}
	var result mcpToolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode tool result: %w", err)
	}
	if result.IsError {
		if len(result.Content) > 0 {
			return "", fmt.Errorf("tool error: %s", result.Content[0].Text)
		}
		return "", fmt.Errorf("tool error")
	}
	var b strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" || part.Type == "" {
			b.WriteString(part.Text)
		}
	}
	return b.String(), nil
}

func (c *MCPClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.id.Add(1)
	req := mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(req); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *MCPClient) notify(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := mcpRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(req)
}

func (c *MCPClient) writeMessage(msg interface{}) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = c.stdin.Write(body)
	return err
}

func (c *MCPClient) readMessage() (mcpRPCResponse, error) {
	var resp mcpRPCResponse
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return resp, err
	}
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return c.readMessage()
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// Close terminates the MCP process.
func (c *MCPClient) Close() error {
	if c == nil {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	return nil
}

type logWriter struct {
	prefix string
}

func (w *logWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		slog.Debug(w.prefix, "stderr", msg)
	}
	return len(p), nil
}
