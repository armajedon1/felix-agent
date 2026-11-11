package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// NetSentinelTool permite auditar equipos en red local y puertos con goroutines concurrentes.
type NetSentinelTool struct{}

func (t *NetSentinelTool) Name() string { return "net_sentinel" }
func (t *NetSentinelTool) Description() string {
	return "Escaneo y auditoria de red ultrarrapido: verifica puertos abiertos en un host, mide latencias TCP o inspecciona dispositivos en la red local y Tailscale."
}
func (t *NetSentinelTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"scan_ports", "tailscale_status", "ping_tcp"},
				"description": "Accion: 'scan_ports' para auditar puertos abiertos, 'tailscale_status' para ver nodos Tailscale o 'ping_tcp' para latencia precisa.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Host o IP objetivo (ej: '127.0.0.1', '192.168.1.1', 'example.com').",
			},
			"ports": map[string]any{
				"type":        "string",
				"description": "Lista de puertos separados por coma o rango (ej: '22,80,443,3000,5070,8045' o 'common').",
			},
		},
		"required": []string{"action"},
	}
}

func (t *NetSentinelTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Ports  string `json:"ports"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	switch params.Action {
	case "tailscale_status":
		cmd := exec.CommandContext(ctx, "tailscale", "status")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error obteniendo tailscale status: %v", err)
		}
		res := strings.TrimSpace(string(out))
		if len(res) > 8000 {
			res = res[:8000] + "\n...(truncado)..."
		}
		return res, nil

	case "ping_tcp":
		if params.Target == "" {
			return "", fmt.Errorf("target requerido para ping_tcp")
		}
		target := params.Target
		if !strings.Contains(target, ":") {
			target += ":80"
		}
		start := time.Now()
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", target)
		elapsed := time.Since(start)
		if err != nil {
			return fmt.Sprintf("Conexion fallida a %s: %v", target, err), nil
		}
		_ = conn.Close()
		return fmt.Sprintf("Conexion TCP exitosa a %s | Latencia: %v", target, elapsed.Round(time.Microsecond)), nil

	case "scan_ports":
		if params.Target == "" {
			params.Target = "127.0.0.1"
		}
		target := params.Target

		portList := []int{22, 53, 80, 443, 3000, 3001, 3306, 5070, 8045, 8080, 25565}
		if params.Ports != "" && params.Ports != "common" {
			portList = nil
			for _, pStr := range strings.Split(params.Ports, ",") {
				var p int
				if _, err := fmt.Sscanf(strings.TrimSpace(pStr), "%d", &p); err == nil && p > 0 && p <= 65535 {
					portList = append(portList, p)
				}
			}
		}

		var openPorts []string
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, p := range portList {
			wg.Add(1)
			go func(port int) {
				defer wg.Done()
				addr := fmt.Sprintf("%s:%d", target, port)
				d := net.Dialer{Timeout: 1 * time.Second}
				conn, err := d.DialContext(ctx, "tcp", addr)
				if err == nil {
					_ = conn.Close()
					mu.Lock()
					openPorts = append(openPorts, fmt.Sprintf("%d (abierto)", port))
					mu.Unlock()
				}
			}(p)
		}
		wg.Wait()

		if len(openPorts) == 0 {
			return fmt.Sprintf("Host %s: ningun puerto abierto detectado entre los analizados.", target), nil
		}
		return fmt.Sprintf("Host %s - Puertos abiertos encontrados:\n- %s", target, strings.Join(openPorts, "\n- ")), nil

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}
