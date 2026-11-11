package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// BackgroundWorkerTool permite ejecutar tareas pesadas o comandos de larga duracion en una goroutine concurrente,
// notificando al usuario por WhatsApp de forma proactiva al terminar, sin bloquear el flujo conversacional.
type BackgroundWorkerTool struct {
	SendFunc   func(chatJID, message string) error
	DefaultJID string
}

func (t *BackgroundWorkerTool) Name() string { return "background_worker" }
func (t *BackgroundWorkerTool) Description() string {
	return "Ejecuta un comando o tarea larga en segundo plano mediante una goroutine concurrente. Responde inmediatamente al usuario y despacha de forma autonoma el resultado completo a WhatsApp al concluir."
}

func (t *BackgroundWorkerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Comando shell a ejecutar en segundo plano.",
			},
			"task_name": map[string]any{
				"type":        "string",
				"description": "Nombre descriptivo de la tarea.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Directorio de trabajo opcional.",
			},
		},
		"required": []string{"command", "task_name"},
	}
}

func (t *BackgroundWorkerTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Command  string `json:"command"`
		TaskName string `json:"task_name"`
		WorkDir  string `json:"workdir"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	targetJID := t.DefaultJID
	if targetJID == "" {
		if sender, ok := ctx.Value("sender_jid").(string); ok && sender != "" {
			targetJID = sender
		}
	}

	// Lanzar ejecucion concurrente
	go func(cmdStr, taskName, workDir, jid string) {
		startTime := time.Now()
		log.Printf("[Background Worker] Iniciando tarea '%s': %s", taskName, cmdStr)

		execCmd := exec.Command("/bin/bash", "-c", cmdStr)
		if workDir != "" {
			execCmd.Dir = workDir
		}

		out, err := execCmd.CombinedOutput()
		duration := time.Since(startTime).Round(time.Second)

		status := "EXITOSA"
		if err != nil {
			status = fmt.Sprintf("FALLIDA (%v)", err)
		}

		outStr := strings.TrimSpace(string(out))
		if len(outStr) > 3000 {
			outStr = outStr[:3000] + "\n...(salida truncada)..."
		}
		if outStr == "" {
			outStr = "(sin salida en terminal)"
		}

		report := fmt.Sprintf("*REPORTE DE TAREA EN SEGUNDO PLANO*\n"+
			"- Tarea: *%s*\n"+
			"- Estado: %s\n"+
			"- Duracion: %s\n"+
			"- Comando: `%s`\n\n"+
			"*Salida del proceso:*\n```\n%s\n```",
			taskName, status, duration, cmdStr, outStr)

		if t.SendFunc != nil {
			if sendErr := t.SendFunc(jid, report); sendErr != nil {
				log.Printf("[Background Worker Error] Fallo notificando a %s: %v", jid, sendErr)
			} else {
				log.Printf("[Background Worker] Notificacion despachada a WhatsApp para tarea '%s'", taskName)
			}
		}
	}(input.Command, input.TaskName, input.WorkDir, targetJID)

	return fmt.Sprintf("Tarea '%s' despachada exitosamente en segundo plano. Te avisare proactivamente por WhatsApp apenas concluya.", input.TaskName), nil
}

// ProcessTool permite listar, inspeccionar o finalizar procesos del sistema en Linux.
type ProcessTool struct{}

func (t *ProcessTool) Name() string { return "process" }
func (t *ProcessTool) Description() string {
	return "Controla e inspecciona procesos en el servidor: listar procesos activos por consumo de memoria/CPU, filtrar por nombre, verificar estado o terminar procesos (kill)."
}
func (t *ProcessTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "kill", "poll"},
				"description": "Accion a realizar: 'list' (listar procesos activos), 'kill' (terminar un PID), 'poll' (verificar estado y metricas de un PID).",
			},
			"pid": map[string]any{
				"type":        "integer",
				"description": "PID del proceso (requerido para action='kill' o 'poll').",
			},
			"filter": map[string]any{
				"type":        "string",
				"description": "Texto para filtrar procesos por nombre o comando (para action='list').",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ProcessTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action string `json:"action"`
		PID    int    `json:"pid"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	switch params.Action {
	case "list":
		cmd := exec.CommandContext(ctx, "ps", "-eo", "pid,user,%cpu,%mem,vsz,rss,comm,args", "--sort=-%mem")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error listando procesos: %w", err)
		}
		lines := strings.Split(string(out), "\n")
		var filtered []string
		header := ""
		if len(lines) > 0 {
			header = lines[0]
			filtered = append(filtered, header)
		}
		filter := strings.ToLower(strings.TrimSpace(params.Filter))
		count := 0
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if filter == "" || strings.Contains(strings.ToLower(l), filter) {
				if len(l) > 120 {
					l = l[:120] + "..."
				}
				filtered = append(filtered, l)
				count++
				if count >= 30 {
					filtered = append(filtered, "...(truncado a los 30 principales procesos)...")
					break
				}
			}
		}
		return strings.Join(filtered, "\n"), nil

	case "poll":
		if params.PID <= 0 {
			return "", errors.New("para action='poll' se requiere un 'pid' valido")
		}
		cmd := exec.CommandContext(ctx, "ps", "-p", fmt.Sprintf("%d", params.PID), "-o", "pid,user,%cpu,%mem,rss,comm,args")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("El proceso PID %d no esta en ejecucion o finalizo.", params.PID), nil
		}
		return strings.TrimSpace(string(out)), nil

	case "kill":
		if params.PID <= 0 {
			return "", errors.New("para action='kill' se requiere un 'pid' valido")
		}
		cmd := exec.CommandContext(ctx, "kill", "-15", fmt.Sprintf("%d", params.PID))
		if out, err := cmd.CombinedOutput(); err != nil {
			// Intentar con SIGKILL si SIGTERM falla
			cmd9 := exec.CommandContext(ctx, "kill", "-9", fmt.Sprintf("%d", params.PID))
			if out9, err9 := cmd9.CombinedOutput(); err9 != nil {
				return "", fmt.Errorf("error terminando PID %d: %s (%v)", params.PID, strings.TrimSpace(string(out9)), err9)
			}
			return fmt.Sprintf("Proceso PID %d finalizado forzosamente con SIGKILL.", params.PID), nil
		} else {
			_ = out
			return fmt.Sprintf("Senal SIGTERM enviada con exito al proceso PID %d.", params.PID), nil
		}

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}
