package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SessionSearchTool permite buscar en el historial de conversaciones de sessions.db.
// Si el usuario es administrador, puede buscar en todos los chats (cross-chat search).
// Si no es administrador, la busqueda se confina exclusivamente a su propia sesion.
type SessionSearchTool struct {
	DB *sql.DB
}

func (t *SessionSearchTool) Name() string { return "session_search" }
func (t *SessionSearchTool) Description() string {
	return "Busca en el historial de conversaciones pasadas (mensajes de usuarios y respuestas del asistente). Para el administrador permite busqueda transversal cross-chat entre todas las sesiones."
}

func (t *SessionSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Termino, frase o concepto clave a buscar en las conversaciones pasadas.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad maxima de mensajes a recuperar (por defecto 5, maximo 20).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.DB == nil {
		return "La base de datos de sesiones no esta disponible.", nil
	}

	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return "Debe especificar un termino de busqueda en 'query'.", nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	isAdmin, _ := ctx.Value("is_admin").(bool)
	currentSession, _ := ctx.Value("session_id").(string)

	var rows *sql.Rows
	var err error

	pattern := "%" + query + "%"

	if isAdmin {
		// Admin: busqueda global cross-chat
		rows, err = t.DB.QueryContext(ctx, `
			SELECT id, session_id, role, content, created_at 
			FROM session_messages 
			WHERE content LIKE ? AND role IN ('user', 'assistant')
			ORDER BY id DESC LIMIT ?;
		`, pattern, limit)
	} else {
		// No-admin: estrictamente limitado a su propio chat/sesion
		if currentSession == "" {
			return "No se pudo determinar la sesion actual del usuario.", nil
		}
		rows, err = t.DB.QueryContext(ctx, `
			SELECT id, session_id, role, content, created_at 
			FROM session_messages 
			WHERE session_id = ? AND content LIKE ? AND role IN ('user', 'assistant')
			ORDER BY id DESC LIMIT ?;
		`, currentSession, pattern, limit)
	}

	if err != nil {
		return fmt.Sprintf("Error consultando historial de sesiones: %v", err), nil
	}
	defer rows.Close()

	type matchItem struct {
		ID        int64
		SessionID string
		Role      string
		Content   string
		CreatedAt string
	}

	var results []matchItem
	for rows.Next() {
		var item matchItem
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &item.CreatedAt); err == nil {
			results = append(results, item)
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No se encontraron mensajes en el historial que coincidan con '%s'.", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Historial encontrado para '%s' (%d mensajes):\n\n", query, len(results)))
	for i, r := range results {
		content := strings.TrimSpace(r.Content)
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		cleanSession := r.SessionID
		if idx := strings.Index(cleanSession, "@"); idx != -1 {
			cleanSession = cleanSession[:idx]
		}
		tParsed, _ := time.Parse("2006-01-02 15:04:05", r.CreatedAt)
		timeStr := r.CreatedAt
		if !tParsed.IsZero() {
			timeStr = tParsed.Format("02/01 15:04")
		}

		sb.WriteString(fmt.Sprintf("[%d] [%s | Chat: %s | Rol: %s]:\n%s\n\n",
			i+1, timeStr, cleanSession, r.Role, content))
	}

	return strings.TrimSpace(sb.String()), nil
}

// SessionHistoryTool recupera el historial cronologico de una sesion especifica.
type SessionHistoryTool struct {
	DB *sql.DB
}

func (t *SessionHistoryTool) Name() string { return "session_history" }
func (t *SessionHistoryTool) Description() string {
	return "Recupera los mensajes anteriores y transcripciones de una sesion o conversacion especifica ordenados cronologicamente."
}
func (t *SessionHistoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "ID de la sesion a inspeccionar (o JID de WhatsApp). Si se omite, inspecciona la sesion actual.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad de mensajes recientes a recuperar (por defecto 10, maximo 50).",
			},
		},
	}
}
func (t *SessionHistoryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.DB == nil {
		return "La base de datos de sesiones no esta disponible.", nil
	}

	var input struct {
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	isAdmin, _ := ctx.Value("is_admin").(bool)
	currentSession, _ := ctx.Value("session_id").(string)

	targetSession := strings.TrimSpace(input.SessionID)
	if targetSession == "" {
		targetSession = currentSession
	}

	// No administradores solo pueden consultar su propia sesion
	if !isAdmin && targetSession != currentSession {
		targetSession = currentSession
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	rows, err := t.DB.QueryContext(ctx,
		`SELECT role, content, created_at FROM session_messages WHERE session_id = ? ORDER BY id DESC LIMIT ?`,
		targetSession, limit,
	)
	if err != nil {
		return "", fmt.Errorf("error consultando historial: %w", err)
	}
	defer rows.Close()

	type msgRecord struct {
		Role      string
		Content   string
		CreatedAt time.Time
	}
	var records []msgRecord
	for rows.Next() {
		var r msgRecord
		var rawDate string
		if err := rows.Scan(&r.Role, &r.Content, &rawDate); err == nil {
			if parsed, err := time.Parse("2006-01-02 15:04:05", rawDate); err == nil {
				r.CreatedAt = parsed
			}
			records = append(records, r)
		}
	}

	if len(records) == 0 {
		return fmt.Sprintf("No hay mensajes registrados para la sesion '%s'.", targetSession), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Historial de sesion '%s' (ultimos %d mensajes) ===\n\n", targetSession, len(records)))
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		loc, err := time.LoadLocation("America/Buenos_Aires")
		if err != nil {
			loc = time.FixedZone("ART", -3*3600)
		}
		timeStr := rec.CreatedAt.In(loc).Format("15:04:05")
		content := rec.Content
		if len(content) > 1200 {
			content = content[:1200] + "...(truncado)..."
		}
		sb.WriteString(fmt.Sprintf("[%s] %s:\n%s\n\n", timeStr, strings.ToUpper(rec.Role), content))
	}

	return strings.TrimSpace(sb.String()), nil
}
