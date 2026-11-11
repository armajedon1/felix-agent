package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// GoalManagerTool permite definir, consultar y actualizar metas u objetivos multi-paso persistentes.
type GoalManagerTool struct {
	DB *sql.DB
}

func (t *GoalManagerTool) Name() string { return "goal_manager" }
func (t *GoalManagerTool) Description() string {
	return "Gestiona metas y objetivos de trabajo persistentes entre turnos: crear un nuevo objetivo ('create'), consultar el objetivo activo ('get'), o actualizar estado o notas ('update' / 'complete')."
}

func (t *GoalManagerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "get", "update", "complete", "list"},
				"description": "Accion a realizar con la meta u objetivo.",
			},
			"objective": map[string]any{
				"type":        "string",
				"description": "Descripcion clara del objetivo o meta a alcanzar (para action='create').",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"in_progress", "completed", "blocked", "cancelled"},
				"description": "Nuevo estado del objetivo (para action='update').",
			},
			"notes": map[string]any{
				"type":        "string",
				"description": "Notas de progreso, bloqueos o conclusiones.",
			},
			"goal_id": map[string]any{
				"type":        "integer",
				"description": "ID del objetivo (opcional, si se omite usa el mas reciente).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *GoalManagerTool) initTable(ctx context.Context) error {
	if t.DB == nil {
		return errors.New("base de datos no inicializada")
	}
	query := `
	CREATE TABLE IF NOT EXISTS agent_goals (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		objective TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'in_progress',
		notes TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := t.DB.ExecContext(ctx, query)
	return err
}

func (t *GoalManagerTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.DB == nil {
		return "Base de datos no disponible para gestion de objetivos.", nil
	}
	if err := t.initTable(ctx); err != nil {
		return "", fmt.Errorf("error inicializando tabla agent_goals: %w", err)
	}

	var params struct {
		Action    string `json:"action"`
		Objective string `json:"objective"`
		Status    string `json:"status"`
		Notes     string `json:"notes"`
		GoalID    int64  `json:"goal_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	sessionID, _ := ctx.Value("session_id").(string)
	if sessionID == "" {
		sessionID = "default"
	}

	switch params.Action {
	case "create":
		obj := strings.TrimSpace(params.Objective)
		if obj == "" {
			return "", errors.New("para action='create' se requiere el parametro 'objective'")
		}
		res, err := t.DB.ExecContext(ctx,
			"INSERT INTO agent_goals (session_id, objective, status, notes) VALUES (?, ?, 'in_progress', ?);",
			sessionID, obj, params.Notes)
		if err != nil {
			return "", fmt.Errorf("error creando objetivo: %w", err)
		}
		id, _ := res.LastInsertId()
		return fmt.Sprintf("Objetivo #%d creado exitosamente: '%s' (Estado: in_progress)", id, obj), nil

	case "get":
		var id int64
		var obj, status, notes, createdAt string
		var err error
		if params.GoalID > 0 {
			err = t.DB.QueryRowContext(ctx,
				"SELECT id, objective, status, notes, created_at FROM agent_goals WHERE id = ?;", params.GoalID).
				Scan(&id, &obj, &status, &notes, &createdAt)
		} else {
			err = t.DB.QueryRowContext(ctx,
				"SELECT id, objective, status, notes, created_at FROM agent_goals WHERE session_id = ? ORDER BY id DESC LIMIT 1;", sessionID).
				Scan(&id, &obj, &status, &notes, &createdAt)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "No hay objetivos activos registrados en esta sesion.", nil
			}
			return "", fmt.Errorf("error consultando objetivo: %w", err)
		}
		return fmt.Sprintf("=== OBJETIVO ACTIVO #%d ===\n- Meta: %s\n- Estado: %s\n- Notas: %s\n- Creado: %s",
			id, obj, status, notes, createdAt), nil

	case "update":
		targetID := params.GoalID
		if targetID <= 0 {
			// Obtener ultimo objetivo activo
			_ = t.DB.QueryRowContext(ctx, "SELECT id FROM agent_goals WHERE session_id = ? ORDER BY id DESC LIMIT 1;", sessionID).Scan(&targetID)
		}
		if targetID <= 0 {
			return "No se encontro un objetivo activo para actualizar.", nil
		}

		newStatus := params.Status
		if newStatus == "" {
			newStatus = "in_progress"
		}

		_, err := t.DB.ExecContext(ctx,
			"UPDATE agent_goals SET status = ?, notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;",
			newStatus, params.Notes, targetID)
		if err != nil {
			return "", fmt.Errorf("error actualizando objetivo: %w", err)
		}
		return fmt.Sprintf("Objetivo #%d actualizado a estado '%s'. Notas: %s", targetID, newStatus, params.Notes), nil

	case "complete":
		targetID := params.GoalID
		if targetID <= 0 {
			_ = t.DB.QueryRowContext(ctx, "SELECT id FROM agent_goals WHERE session_id = ? ORDER BY id DESC LIMIT 1;", sessionID).Scan(&targetID)
		}
		if targetID <= 0 {
			return "No se encontro un objetivo activo para completar.", nil
		}
		_, err := t.DB.ExecContext(ctx,
			"UPDATE agent_goals SET status = 'completed', notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;",
			params.Notes, targetID)
		if err != nil {
			return "", fmt.Errorf("error completando objetivo: %w", err)
		}
		return fmt.Sprintf("Objetivo #%d marcado como COMPLETADO con exito.", targetID), nil

	case "list":
		rows, err := t.DB.QueryContext(ctx,
			"SELECT id, objective, status, notes, updated_at FROM agent_goals WHERE session_id = ? ORDER BY id DESC LIMIT 10;", sessionID)
		if err != nil {
			return "", fmt.Errorf("error listando objetivos: %w", err)
		}
		defer rows.Close()

		var sb strings.Builder
		sb.WriteString("=== HISTORIAL DE OBJETIVOS ===\n\n")
		count := 0
		for rows.Next() {
			var id int64
			var obj, status, notes, upd string
			if err := rows.Scan(&id, &obj, &status, &notes, &upd); err == nil {
				sb.WriteString(fmt.Sprintf("[%d] [%s] %s\n    Notas: %s (%s)\n\n", id, strings.ToUpper(status), obj, notes, upd))
				count++
			}
		}
		if count == 0 {
			return "No hay objetivos registrados.", nil
		}
		return strings.TrimSpace(sb.String()), nil

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}
