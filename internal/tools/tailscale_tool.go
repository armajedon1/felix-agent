package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// TailscaleTool inspecciona el estado, topologia y nodos de la red privada Tailscale.
type TailscaleTool struct{}

func (t *TailscaleTool) Name() string { return "tailscale_tool" }
func (t *TailscaleTool) Description() string {
	return "Inspecciona la topologia y conectividad de la red privada Tailscale: nodos activos, workstation Lenovo, latencia y verificacion de rutas."
}

func (t *TailscaleTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "peers", "ping", "ip"},
				"description": "Operacion: 'status' (resumen de red), 'peers' (lista de dispositivos), 'ping' (probar nodo), 'ip' (mostrar IP de este servidor).",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Nombre de nodo o direccion IP para action='ping' (ej: '100.x.y.z' o 'servidor-remoto').",
			},
		},
		"required": []string{"action"},
	}
}

type tsStatusJSON struct {
	Self struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		Online       bool     `json:"Online"`
		HostName     string   `json:"HostName"`
		OS           string   `json:"OS"`
	} `json:"Self"`
	Peer map[string]struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		Online       bool     `json:"Online"`
		HostName     string   `json:"HostName"`
		OS           string   `json:"OS"`
		Relay        string   `json:"Relay"`
		RxBytes      int64    `json:"RxBytes"`
		TxBytes      int64    `json:"TxBytes"`
		LastSeen     string   `json:"LastSeen"`
		CurAddr      string   `json:"CurAddr"`
	} `json:"Peer"`
}

func (t *TailscaleTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	switch params.Action {
	case "ip":
		cmd := exec.CommandContext(timeoutCtx, "tailscale", "ip", "-4")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("error obteniendo IP: %v (%s)", err, string(out))
		}
		ip := strings.TrimSpace(string(out))
		return fmt.Sprintf("IP Tailscale de este servidor: `%s`", ip), nil

	case "ping":
		target := strings.TrimSpace(params.Target)
		if target == "" {
			return "Debe especificar un target (nombre o IP del nodo) para action='ping'.", nil
		}
		cmd := exec.CommandContext(timeoutCtx, "tailscale", "ping", "-c", "3", target)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("Fallo ping a `%s`: %v\n%s", target, err, string(out)), nil
		}
		return fmt.Sprintf("*PING TAILSCALE A `%s`*\n```\n%s```", target, strings.TrimSpace(string(out))), nil

	case "status", "peers":
		cmd := exec.CommandContext(timeoutCtx, "tailscale", "status", "--json")
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Fallback a texto plano si falla JSON
			cmdText := exec.CommandContext(timeoutCtx, "tailscale", "status")
			outText, errText := cmdText.CombinedOutput()
			if errText != nil {
				return "", fmt.Errorf("error consultando estado Tailscale: %v", errText)
			}
			return fmt.Sprintf("*ESTADO TAILSCALE*\n```\n%s```", strings.TrimSpace(string(outText))), nil
		}

		var ts tsStatusJSON
		if err := json.Unmarshal(out, &ts); err != nil {
			return string(out), nil
		}

		var sb strings.Builder
		sb.WriteString("*TOPOLOGIA DE RED PRIVADA TAILSCALE*\n")
		selfIP := ""
		if len(ts.Self.TailscaleIPs) > 0 {
			selfIP = ts.Self.TailscaleIPs[0]
		}
		sb.WriteString(fmt.Sprintf("- *Nodo Local (Servidor):* `%s` (`%s`, %s)\n\n", ts.Self.HostName, selfIP, ts.Self.OS))
		sb.WriteString("*Dispositivos en la Malla:*\n")

		for _, peer := range ts.Peer {
			peerIP := ""
			if len(peer.TailscaleIPs) > 0 {
				peerIP = peer.TailscaleIPs[0]
			}
			statusStr := "OFFLINE"
			if peer.Online {
				statusStr = "ONLINE"
			}
			connType := "directo"
			if peer.CurAddr == "" {
				connType = fmt.Sprintf("relay %s", peer.Relay)
			}
			sb.WriteString(fmt.Sprintf("- *%s* (`%s` | %s)\n  - Estado: *%s* (%s)\n",
				peer.HostName, peerIP, peer.OS, statusStr, connType))
		}

		return sb.String(), nil

	default:
		return "", fmt.Errorf("accion '%s' no reconocida", params.Action)
	}
}
