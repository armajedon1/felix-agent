package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"felix-go/internal/minecraft"
)

// MinecraftTool implementa el control del servidor de Minecraft a traves de RCON y PM2.
type MinecraftTool struct {
	client *minecraft.Client
}

// NewMinecraftTool inicializa la herramienta con el cliente RCON predeterminado.
func NewMinecraftTool() *MinecraftTool {
	return &MinecraftTool{
		client: minecraft.DefaultClient(),
	}
}

func (t *MinecraftTool) Name() string { return "minecraft_control" }

func (t *MinecraftTool) Description() string {
	return "Control integral y administracion del servidor Minecraft del homelab via RCON y PM2: consultar estado general y jugadores online, gestionar whitelist, difundir mensajes en chat, expulsar jugadores y ejecutar comandos directos."
}

func (t *MinecraftTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "list", "whitelist", "say", "kick", "command"},
				"description": "Accion a realizar: 'status' (estado PM2 y jugadores), 'list' (jugadores online), 'whitelist' (consultar o modificar whitelist), 'say' (mensaje global), 'kick' (expulsar), 'command' (comando RCON directo).",
			},
			"subaction": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "add", "remove", "reload"},
				"description": "Subaccion opcional para whitelist: 'list', 'add', 'remove', 'reload'.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Nombre del jugador objetivo (para whitelist add/remove o kick).",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Mensaje para difusion en el chat de Minecraft (cuando action='say').",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Comando RCON a ejecutar en el servidor (cuando action='command').",
			},
		},
		"required": []string{"action"},
	}
}

func (t *MinecraftTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action    string `json:"action"`
		Subaction string `json:"subaction"`
		Target    string `json:"target"`
		Message   string `json:"message"`
		Command   string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	client := t.client
	if client == nil {
		client = minecraft.DefaultClient()
	}

	switch params.Action {
	case "status":
		return t.handleStatus(ctx, client)

	case "list":
		out, err := client.ListPlayers()
		if err != nil {
			return "", fmt.Errorf("error consultando jugadores online: %w", err)
		}
		return out, nil

	case "whitelist":
		sub := strings.ToLower(params.Subaction)
		switch sub {
		case "add":
			if params.Target == "" {
				return "", errors.New("target es requerido para whitelist add")
			}
			return client.WhitelistAdd(params.Target)
		case "remove":
			if params.Target == "" {
				return "", errors.New("target es requerido para whitelist remove")
			}
			return client.WhitelistRemove(params.Target)
		case "reload":
			return client.WhitelistReload()
		default:
			return client.WhitelistList()
		}

	case "say":
		if params.Message == "" {
			return "", errors.New("message es requerido para action='say'")
		}
		return client.Say(params.Message)

	case "kick":
		if params.Target == "" {
			return "", errors.New("target es requerido para action='kick'")
		}
		return client.Kick(params.Target)

	case "command":
		if params.Command == "" {
			return "", errors.New("command es requerido para action='command'")
		}
		return client.Execute(params.Command)

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}

func (t *MinecraftTool) handleStatus(ctx context.Context, client *minecraft.Client) (string, error) {
	var sb strings.Builder
	sb.WriteString("=== ESTADO DEL SERVIDOR MINECRAFT ===\n")

	// 1. Estado en PM2
	pm2Status := "DESCONOCIDO"
	pm2PID := 0
	pm2MemMB := 0.0
	pm2Restarts := 0
	pm2UpStr := "N/D"

	if out, err := exec.CommandContext(ctx, "pm2", "jlist").Output(); err == nil {
		type pm2Monit struct {
			Memory float64 `json:"memory"`
			CPU    float64 `json:"cpu"`
		}
		type pm2Env struct {
			Status      string `json:"status"`
			Uptime      int64  `json:"pm_uptime"`
			RestartTime int    `json:"restart_time"`
		}
		type pm2Item struct {
			Name   string   `json:"name"`
			PID    int      `json:"pid"`
			Monit  pm2Monit `json:"monit"`
			PM2Env pm2Env   `json:"pm2_env"`
		}
		var items []pm2Item
		if err := json.Unmarshal(out, &items); err == nil {
			for _, it := range items {
				if it.Name == "minecraft" {
					pm2Status = strings.ToUpper(it.PM2Env.Status)
					pm2PID = it.PID
					pm2MemMB = it.Monit.Memory / (1024 * 1024)
					pm2Restarts = it.PM2Env.RestartTime
					if it.PM2Env.Uptime > 0 {
						diff := time.Since(time.UnixMilli(it.PM2Env.Uptime))
						if diff.Hours() >= 24 {
							pm2UpStr = fmt.Sprintf("%.0fd", diff.Hours()/24)
						} else if diff.Hours() >= 1 {
							pm2UpStr = fmt.Sprintf("%.0fh", diff.Hours())
						} else {
							pm2UpStr = fmt.Sprintf("%.0fm", diff.Minutes())
						}
					}
					break
				}
			}
		}
	}

	sb.WriteString(fmt.Sprintf("PM2: %s | PID: %d | RAM: %.1f MB | Uptime: %s | Reinicios: %d\n",
		pm2Status, pm2PID, pm2MemMB, pm2UpStr, pm2Restarts))

	// 2. Consulta RCON (jugadores online)
	rconOut, err := client.ListPlayers()
	if err != nil {
		sb.WriteString(fmt.Sprintf("RCON (127.0.0.1:25575): No disponible (%v)\n", err))
		sb.WriteString("Nota: Si el servidor utiliza lazymc, se reactivara cuando un jugador intente conectarse.")
	} else {
		sb.WriteString(fmt.Sprintf("RCON: Conectado | %s", rconOut))
	}

	return sb.String(), nil
}
