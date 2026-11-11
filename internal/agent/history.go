package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	_ "modernc.org/sqlite"
)

// StoredMessage representa un mensaje almacenado con su ID en base de datos.
type StoredMessage struct {
	ID         int64
	Role       string
	Content    string
	ToolCalls  string
	ToolCallID string
}

// HistoryManager gestiona el historial conversacional persistente y los resumenes compactados en SQLite.
// Permite que la memoria sobreviva a reinicios de PM2 y soporte charlas indefinidas mediante compactacion semantica.
type HistoryManager struct {
	mu     sync.Mutex
	db     *sql.DB
	maxLen int
}

// NewHistoryManager inicializa o abre la base de datos de sesiones en SQLite.
func NewHistoryManager(dbPath string, maxLen int) (*HistoryManager, error) {
	if maxLen <= 0 {
		maxLen = 30
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error abriendo base de sesiones SQLite %s: %w", dbPath, err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS session_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		tool_calls TEXT DEFAULT '',
		tool_call_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_session_messages_session ON session_messages(session_id, id);

	CREATE TABLE IF NOT EXISTS session_summaries (
		session_id TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		compacted_until_id INTEGER NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("error inicializando tablas de sesiones: %w", err)
	}

	return &HistoryManager{
		db:     db,
		maxLen: maxLen,
	}, nil
}

// GetSummary obtiene el resumen semantico consolidado de una sesion si existe.
func (h *HistoryManager) GetSummary(sessionID string) (string, int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var summary string
	var untilID int64
	query := `SELECT summary, compacted_until_id FROM session_summaries WHERE session_id = ?;`
	err := h.db.QueryRow(query, sessionID).Scan(&summary, &untilID)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return summary, untilID, nil
}

// SaveSummary guarda o actualiza el resumen semantico consolidado de la sesion.
func (h *HistoryManager) SaveSummary(sessionID, summary string, untilID int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	query := `
	INSERT INTO session_summaries (session_id, summary, compacted_until_id, updated_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(session_id) DO UPDATE SET
		summary = excluded.summary,
		compacted_until_id = excluded.compacted_until_id,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := h.db.Exec(query, sessionID, summary, untilID)
	return err
}

// Get obtiene los ultimos N mensajes recientes de una sesion en orden cronologico ascendente.
func (h *HistoryManager) Get(sessionID string) []OpenAIMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	query := `
	SELECT role, content, tool_calls, tool_call_id
	FROM session_messages
	WHERE session_id = ?
	ORDER BY id DESC
	LIMIT ?;
	`
	rows, err := h.db.Query(query, sessionID, h.maxLen)
	if err != nil {
		log.Printf("[History Error] Fallo consultando historial de %s: %v", sessionID, err)
		return nil
	}
	defer rows.Close()

	var reverseMsgs []OpenAIMessage
	for rows.Next() {
		var role, content, toolCallsJSON, toolCallID string
		if err := rows.Scan(&role, &content, &toolCallsJSON, &toolCallID); err != nil {
			continue
		}

		msg := OpenAIMessage{
			Role:       role,
			Content:    content,
			ToolCallID: toolCallID,
		}
		if toolCallsJSON != "" {
			var tcs []ToolCall
			if err := json.Unmarshal([]byte(toolCallsJSON), &tcs); err == nil {
				msg.ToolCalls = tcs
			}
		}
		reverseMsgs = append(reverseMsgs, msg)
	}

	n := len(reverseMsgs)
	msgs := make([]OpenAIMessage, n)
	for i, m := range reverseMsgs {
		msgs[n-1-i] = m
	}
	return msgs
}

// GetOldestMessages obtiene los primeros N mensajes cronologicos de una sesion (para compactacion).
func (h *HistoryManager) GetOldestMessages(sessionID string, count int) ([]StoredMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	query := `
	SELECT id, role, content, tool_calls, tool_call_id
	FROM session_messages
	WHERE session_id = ?
	ORDER BY id ASC
	LIMIT ?;
	`
	rows, err := h.db.Query(query, sessionID, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// DeleteMessagesUpTo elimina mensajes antiguos que ya han sido compactados en el resumen.
func (h *HistoryManager) DeleteMessagesUpTo(sessionID string, maxID int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	query := `DELETE FROM session_messages WHERE session_id = ? AND id <= ?;`
	_, err := h.db.Exec(query, sessionID, maxID)
	return err
}

// Append persiste un mensaje en SQLite para la sesion indicada.
func (h *HistoryManager) Append(sessionID string, msg OpenAIMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	toolCallsJSON := ""
	if len(msg.ToolCalls) > 0 {
		if b, err := json.Marshal(msg.ToolCalls); err == nil {
			toolCallsJSON = string(b)
		}
	}

	query := `
	INSERT INTO session_messages (session_id, role, content, tool_calls, tool_call_id)
	VALUES (?, ?, ?, ?, ?);
	`
	if _, err := h.db.Exec(query, sessionID, msg.Role, msg.Content, toolCallsJSON, msg.ToolCallID); err != nil {
		log.Printf("[History Error] Fallo insertando mensaje en SQLite para %s: %v", sessionID, err)
	}
}

// Clear borra todo el historial y resumen de una sesion (reseteo total de contexto).
func (h *HistoryManager) Clear(sessionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, err1 := h.db.Exec(`DELETE FROM session_messages WHERE session_id = ?;`, sessionID)
	_, err2 := h.db.Exec(`DELETE FROM session_summaries WHERE session_id = ?;`, sessionID)
	if err1 != nil {
		return err1
	}
	return err2
}

// Count retorna la cantidad total de mensajes guardados para una sesion.
func (h *HistoryManager) Count(sessionID string) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var count int
	query := `SELECT COUNT(*) FROM session_messages WHERE session_id = ?;`
	err := h.db.QueryRow(query, sessionID).Scan(&count)
	return count, err
}

// TotalCharacters calcula la longitud acumulada en caracteres de los mensajes de la sesion.
func (h *HistoryManager) TotalCharacters(sessionID string) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var total int
	query := `SELECT COALESCE(SUM(length(content)), 0) FROM session_messages WHERE session_id = ?;`
	err := h.db.QueryRow(query, sessionID).Scan(&total)
	return total, err
}

// DB retorna la conexion SQLite subyacente.
func (h *HistoryManager) DB() *sql.DB {
	return h.db
}

// Close cierra la conexion con SQLite.
func (h *HistoryManager) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		return h.db.Close()
	}
	return nil
}
