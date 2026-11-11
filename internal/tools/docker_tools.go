package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// DockerTool permite inspeccionar y controlar contenedores Docker de forma directa y rapida.
type DockerTool struct{}

func (t *DockerTool) Name() string { return "docker" }
func (t *DockerTool) Description() string {
	return "Inspecciona el estado, logs y metricas de contenedores y stacks Docker en el homelab."
}
func (t *DockerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"ps", "stats", "logs", "restart"},
				"description": "Accion: 'ps' lista contenedores activos, 'stats' metricas de CPU/RAM, 'logs' ultimas lineas, 'restart' reinicia un contenedor.",
			},
			"container": map[string]any{
				"type":        "string",
				"description": "Nombre o ID del contenedor (para logs o restart).",
			},
			"lines": map[string]any{
				"type":        "integer",
				"description": "Cantidad de lineas de logs (por defecto 30).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *DockerTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action    string `json:"action"`
		Container string `json:"container"`
		Lines     int    `json:"lines"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	switch params.Action {
	case "ps":
		cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}\t{{.Image}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error ejecutando docker ps: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil

	case "stats":
		cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error ejecutando docker stats: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil

	case "logs":
		if params.Container == "" {
			return "", fmt.Errorf("parametro 'container' es obligatorio para logs")
		}
		lines := params.Lines
		if lines <= 0 {
			lines = 30
		}
		cmd := exec.CommandContext(ctx, "docker", "logs", fmt.Sprintf("--tail=%d", lines), params.Container)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error obteniendo logs de %s: %v (%s)", params.Container, err, strings.TrimSpace(string(out)))
		}
		res := strings.TrimSpace(string(out))
		if len(res) > 8000 {
			res = res[len(res)-8000:]
		}
		return res, nil

	case "restart":
		if params.Container == "" {
			return "", fmt.Errorf("parametro 'container' es obligatorio para restart")
		}
		cmd := exec.CommandContext(ctx, "docker", "restart", params.Container)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error reiniciando %s: %v (%s)", params.Container, err, strings.TrimSpace(string(out)))
		}
		return fmt.Sprintf("Contenedor %s reiniciado exitosamente.", params.Container), nil

	default:
		return "", fmt.Errorf("accion docker desconocida: %s", params.Action)
	}
}
