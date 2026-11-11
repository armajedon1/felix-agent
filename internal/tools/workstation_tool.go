package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WorkstationTool gestiona el control, estado, sincronizacion y encolado hacia una maquina remota.
type WorkstationTool struct {
	Host string // Usuario y host/IP (ej: "usuario@192.168.1.50")
}

func (t *WorkstationTool) Name() string { return "workstation_link" }
func (t *WorkstationTool) Description() string {
	return "Control integral y monitoreo de workstation o equipo secundario remoto: estado en vivo (online/offline/carga), sincronizacion de archivos/proyectos via rsync y administracion de cola de tareas."
}

func (t *WorkstationTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "sync", "queue_cmd", "queue_agy", "task_list", "task_show", "task_cancel"},
				"description": "Accion a realizar con la workstation.",
			},
			"direction": map[string]any{
				"type":        "string",
				"enum":        []string{"to_workstation", "to_server"},
				"description": "Direccion de sincronizacion para action='sync': 'to_workstation' (Servidor -> PC) o 'to_server' (PC -> Servidor).",
			},
			"source_path": map[string]any{
				"type":        "string",
				"description": "Ruta origen de archivos o carpetas a sincronizar.",
			},
			"dest_path": map[string]any{
				"type":        "string",
				"description": "Ruta destino de sincronizacion.",
			},
			"exclude": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Patrones a excluir en la sincronizacion.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Comando bash a encolar para action='queue_cmd'.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Instrucciones de desarrollo para agy en action='queue_agy'.",
			},
			"project": map[string]any{
				"type":        "string",
				"description": "Nombre de subcarpeta de proyecto en la workstation.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Titulo descriptivo de la tarea.",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "ID de la tarea para action='task_show' o 'task_cancel'.",
			},
		},
		"required": []string{"action"},
	}
}

func getPcTaskBin() string {
	if p, err := exec.LookPath("pc-task"); err == nil {
		return p
	}
	if home, _ := os.UserHomeDir(); home != "" {
		localBin := filepath.Join(home, ".local", "bin", "pc-task")
		if _, err := os.Stat(localBin); err == nil {
			return localBin
		}
	}
	return "pc-task"
}

func (t *WorkstationTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Host == "" {
		return "Workstation remota no configurada en config.json ('workstation_host'). Para habilitarla, configure el host o ejecute /config workstation <user@ip>.", nil
	}

	var params struct {
		Action     string   `json:"action"`
		Direction  string   `json:"direction"`
		SourcePath string   `json:"source_path"`
		DestPath   string   `json:"dest_path"`
		Exclude    []string `json:"exclude"`
		Command    string   `json:"command"`
		Prompt     string   `json:"prompt"`
		Project    string   `json:"project"`
		Title      string   `json:"title"`
		TaskID     string   `json:"task_id"`
	}

	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	pcTaskBin := getPcTaskBin()

	switch params.Action {
	case "status":
		return t.handleStatus(ctx)
	case "sync":
		return t.handleSync(ctx, params.Direction, params.SourcePath, params.DestPath, params.Exclude)
	case "queue_cmd":
		if params.Command == "" {
			return "", errors.New("command es requerido para queue_cmd")
		}
		title := params.Title
		if title == "" {
			title = params.Command
		}
		cmd := exec.CommandContext(ctx, pcTaskBin, "add", title, params.Command)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("error encolando comando: %w", err)
		}
		return string(out), nil
	case "queue_agy":
		if params.Prompt == "" {
			return "", errors.New("prompt es requerido para queue_agy")
		}
		title := params.Title
		if title == "" {
			title = "Desarrollo con agy"
		}
		cmdArgs := []string{"agy", title, params.Prompt}
		if params.Project != "" {
			cmdArgs = append(cmdArgs, "--project", params.Project)
		}
		cmd := exec.CommandContext(ctx, pcTaskBin, cmdArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("error encolando tarea agy: %w", err)
		}
		return string(out), nil
	case "task_list":
		cmd := exec.CommandContext(ctx, pcTaskBin, "list")
		out, _ := cmd.CombinedOutput()
		lines := strings.Split(string(out), "\n")
		if len(lines) > 20 {
			lines = lines[:20]
		}
		return strings.Join(lines, "\n"), nil
	case "task_show":
		if params.TaskID == "" {
			return "", errors.New("task_id es requerido")
		}
		cmd := exec.CommandContext(ctx, pcTaskBin, "show", params.TaskID)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("error consultando tarea %s: %w", params.TaskID, err)
		}
		return string(out), nil
	case "task_cancel":
		if params.TaskID == "" {
			return "", errors.New("task_id es requerido")
		}
		cmd := exec.CommandContext(ctx, pcTaskBin, "cancel", params.TaskID)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("error cancelando tarea %s: %w", params.TaskID, err)
		}
		return string(out), nil
	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}

func (t *WorkstationTool) handleStatus(ctx context.Context) (string, error) {
	var report strings.Builder
	report.WriteString("=== ESTADO DE WORKSTATION REMOTA ===\n")
	report.WriteString(fmt.Sprintf("Host configurado: %s\n", t.Host))

	// 1. Comprobacion de conectividad SSH directa (timeout 2s)
	sshCtx, sshCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer sshCancel()

	sshCmd := exec.CommandContext(sshCtx, "ssh", "-o", "ConnectTimeout=2", "-o", "BatchMode=yes", t.Host, "uptime; upower -i $(upower -e | grep BAT | head -n1) 2>/dev/null | grep -E 'state|percentage|time to empty'")
	sshOut, err := sshCmd.CombinedOutput()
	if err == nil && len(sshOut) > 0 {
		report.WriteString("\n[Enlace SSH en Vivo: CONECTADO]\n")
		report.WriteString(strings.TrimSpace(string(sshOut)) + "\n")
	} else {
		report.WriteString("\n[Enlace SSH en Vivo: DESCONECTADO / SUSPENDIDO]\n")
	}

	// 2. Tareas en cola
	pcTaskBin := getPcTaskBin()
	queueCmd := exec.CommandContext(ctx, pcTaskBin, "list")
	queueOut, _ := queueCmd.CombinedOutput()
	var pending []string
	for _, l := range strings.Split(string(queueOut), "\n") {
		if strings.Contains(l, "pending") || strings.Contains(l, "running") {
			pending = append(pending, l)
		}
	}
	report.WriteString(fmt.Sprintf("\nTareas activas/pendientes en cola (%d):\n", len(pending)))
	if len(pending) == 0 {
		report.WriteString("No hay tareas pendientes en la cola.\n")
	} else {
		for _, p := range pending {
			report.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}

	return report.String(), nil
}

func (t *WorkstationTool) handleSync(ctx context.Context, direction, src, dest string, excludes []string) (string, error) {
	if src == "" || dest == "" {
		return "", errors.New("source_path y dest_path son obligatorios para sync")
	}

	var rsyncArgs []string
	rsyncArgs = append(rsyncArgs, "-avz", "--delete")

	for _, ex := range excludes {
		rsyncArgs = append(rsyncArgs, "--exclude", ex)
	}
	rsyncArgs = append(rsyncArgs,
		"--exclude", ".git",
		"--exclude", "node_modules",
		"--exclude", ".venv",
		"--exclude", "dist",
		"--exclude", "build",
	)

	switch direction {
	case "to_workstation":
		rsyncArgs = append(rsyncArgs, src, fmt.Sprintf("%s:%s", t.Host, dest))
	case "to_server":
		rsyncArgs = append(rsyncArgs, fmt.Sprintf("%s:%s", t.Host, src), dest)
	default:
		return "", fmt.Errorf("direccion invalida '%s': debe ser 'to_workstation' o 'to_server'", direction)
	}

	cmd := exec.CommandContext(ctx, "rsync", rsyncArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("error sincronizando via rsync: %w\n%s", err, string(out))
	}

	lines := strings.Split(string(out), "\n")
	summary := lines
	if len(lines) > 20 {
		summary = append(lines[:10], fmt.Sprintf("... (%d archivos sincronizados) ...", len(lines)-15))
		summary = append(summary, lines[len(lines)-5:]...)
	}

	return fmt.Sprintf("Sincronizacion '%s' completada con exito:\n%s", direction, strings.Join(summary, "\n")), nil
}
