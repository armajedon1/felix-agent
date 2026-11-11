package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// ServerConfig especifica la configuracion para lanzar un servidor MCP via STDIO.
type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// JSONRPCRequest modela una peticion JSON-RPC 2.0.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse modela una respuesta JSON-RPC 2.0.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// MCPToolDefinition representa una herramienta expuesta por un servidor MCP.
type MCPToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Client gestiona la comunicacion STDIO bidireccional con un proceso servidor MCP.
type Client struct {
	name       string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	seq        int64
	pendingMu  sync.Mutex
	pending    map[int64]chan JSONRPCResponse
	closed     chan struct{}
	closeOnce  sync.Once
}

// StartClient lanza un proceso de servidor MCP e inicializa el protocolo JSON-RPC.
func StartClient(ctx context.Context, name string, cfg ServerConfig) (*Client, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("error obteniendo stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("error obteniendo stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("error iniciando proceso MCP '%s': %w", name, err)
	}

	c := &Client{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int64]chan JSONRPCResponse),
		closed:  make(chan struct{}),
	}

	go c.readLoop()

	// 1. Enviar initialize
	initReq := map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]any{
			"name":    "Felix-Go",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	}
	_, err = c.Call(ctx, "initialize", initReq)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("fallo initialize en servidor MCP '%s': %w", name, err)
	}

	// 2. Enviar notificacion initialized
	_ = c.Notify("notifications/initialized", map[string]any{})

	log.Printf("[MCP Client] Servidor '%s' inicializado y listo via STDIO", name)
	return c, nil
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	const maxScanBuffer = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScanBuffer)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- resp:
			default:
			}
		}
	}

	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
}

// Call invoca un metodo JSON-RPC 2.0 y espera la respuesta.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.seq, 1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	ch := make(chan JSONRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if _, err := c.stdin.Write(data); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("error escribiendo en stdin de MCP: %w", err)
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("cliente MCP cerrado")
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("conexion MCP cerrada antes de recibir respuesta")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("error MCP %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Notify envia una notificacion JSON-RPC unidireccional (sin ID).
func (c *Client) Notify(method string, params any) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	_, err = c.stdin.Write(data)
	return err
}

// ListTools consulta las herramientas disponibles mediante 'tools/list'.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolDefinition, error) {
	result, err := c.Call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Tools []MCPToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, fmt.Errorf("error deserializando tools/list: %w", err)
	}

	return parsed.Tools, nil
}

// CallTool invoca una herramienta mediante 'tools/call'.
func (c *Client) CallTool(ctx context.Context, name string, arguments any) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}

	result, err := c.Call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return string(result), nil
	}

	var sb strings.Builder
	for _, item := range parsed.Content {
		if item.Type == "text" || item.Text != "" {
			sb.WriteString(item.Text)
		}
	}

	res := sb.String()
	if parsed.IsError {
		return res, fmt.Errorf("error devuelto por herramienta MCP '%s': %s", name, res)
	}

	return res, nil
}

// Close finaliza el cliente y el subproceso.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
	return nil
}
