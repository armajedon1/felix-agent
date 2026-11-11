package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"felix-go/internal/healer"
)

// InfraTool provee capacidades agenticas para diagnosticar, curar y versionar el estado del homelab.
type InfraTool struct {
	healer  *healer.Engine
	snapMgr *healer.SnapshotManager
}

// NewInfraTool inicializa la herramienta con el motor de auto-curacion y persistencia de snapshots.
func NewInfraTool(dbPath string) (*InfraTool, error) {
	sm, err := healer.NewSnapshotManager(dbPath)
	if err != nil {
		return nil, fmt.Errorf("error inicializando SnapshotManager: %w", err)
	}

	return &InfraTool{
		healer:  healer.NewEngine(),
		snapMgr: sm,
	}, nil
}

func (t *InfraTool) Name() string { return "infra_healer" }

func (t *InfraTool) Description() string {
	return "Diagnostico profundo, auto-curacion de servicios caidos (PM2 y Docker), captura de snapshots de infraestructura y comparacion de diferencias (diff)."
}

func (t *InfraTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"check", "heal", "snapshot", "diff"},
				"description": "Accion a realizar: 'check' (diagnostico sin cambios), 'heal' (auto-curacion de procesos y contenedores caidos), 'snapshot' (guardar estado actual), 'diff' (comparar estado en vivo con el ultimo snapshot guardado).",
			},
			"service": map[string]any{
				"type":        "string",
				"description": "Nombre opcional del servicio o contenedor especifico a analizar o curar (ej: 'web', 'db', 'nginx'). Si se omite, opera sobre todos los servicios criticos.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *InfraTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action  string `json:"action"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	switch strings.ToLower(params.Action) {
	case "check":
		rep, err := t.healer.Check(ctx, params.Service)
		if err != nil {
			return "", fmt.Errorf("error en diagnostico: %w", err)
		}
		return rep.FormatMarkdown(), nil

	case "heal":
		rep, err := t.healer.Heal(ctx, params.Service)
		if err != nil {
			return "", fmt.Errorf("error en auto-curacion: %w", err)
		}
		return rep.FormatMarkdown(), nil

	case "snapshot":
		snap, err := t.snapMgr.TakeSnapshot(ctx)
		if err != nil {
			return "", fmt.Errorf("error tomando snapshot: %w", err)
		}
		return fmt.Sprintf("Snapshot #%d registrado con exito (%s):\n"+
			"- Puertos TCP abiertos: %d\n"+
			"- Servicios PM2: %d\n"+
			"- Contenedores Docker: %d\n"+
			"- RAM Usada: %d MB / %d MB\n"+
			"- Almacenamiento SSD libre: %s",
			snap.ID, snap.Timestamp.Format("2006-01-02 15:04:05"),
			len(snap.TCPPorts), len(snap.PM2Services), len(snap.DockerContainers),
			snap.Metrics.RAMUsedMB, snap.Metrics.RAMTotalMB, snap.Metrics.DiskFree), nil

	case "diff":
		latest, err := t.snapMgr.GetLatestSnapshot(ctx)
		if err != nil {
			return "", fmt.Errorf("error obteniendo ultimo snapshot: %w", err)
		}
		if latest == nil {
			// No hay snapshots previos, tomar uno base ahora
			newSnap, err := t.snapMgr.TakeSnapshot(ctx)
			if err != nil {
				return "", fmt.Errorf("error generando snapshot inicial: %w", err)
			}
			return fmt.Sprintf("No habia snapshots previos. Se ha generado y guardado el Snapshot base #%d.", newSnap.ID), nil
		}

		current := &healer.Snapshot{
			ID:               latest.ID + 1,
			Timestamp:        time.Now(),
			TCPPorts:         healer.CollectTCPPorts(ctx),
			PM2Services:      healer.CollectPM2Services(ctx),
			DockerContainers: healer.CollectDockerContainers(ctx),
			Metrics:          healer.CollectSystemMetrics(ctx),
		}

		return healer.CompareSnapshots(latest, current), nil

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}
