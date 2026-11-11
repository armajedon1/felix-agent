package scratchpad

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ScratchpadItem representa el estado de trabajo actual de una sesion.
type ScratchpadItem struct {
	SessionID string            `json:"session_id"`
	Goal      string            `json:"goal"`
	Status    string            `json:"status"`
	Variables map[string]string `json:"variables"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Manager gestiona la memoria de trabajo persistente por sesion en SQLite.
// Permite que el agente mantenga el hilo de objetivos complejos entre turnos sin re-explicaciones.
type Manager struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewManager inicializa la tabla de scratchpad en la base de datos de sesiones.
func NewManager(db *sql.DB) (*Manager, error) {
	query := `
	CREATE TABLE IF NOT EXISTS scratchpad (
		session_id TEXT PRIMARY KEY,
		goal TEXT NOT NULL,
		status TEXT NOT NULL,
		variables TEXT DEFAULT '{}',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("error inicializando tabla scratchpad: %w", err)
	}

	return &Manager{db: db}, nil
}

// Get recupera el item activo del scratchpad para una sesion.
func (sm *Manager) Get(sessionID string) (*ScratchpadItem, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var item ScratchpadItem
	var varsJSON string
	var updatedStr string

	query := `SELECT session_id, goal, status, variables, updated_at FROM scratchpad WHERE session_id = ?;`
	err := sm.db.QueryRow(query, sessionID).Scan(&item.SessionID, &item.Goal, &item.Status, &varsJSON, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	item.Variables = make(map[string]string)
	if varsJSON != "" {
		_ = json.Unmarshal([]byte(varsJSON), &item.Variables)
	}
	item.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
	return &item, nil
}

// Set guarda o actualiza el objetivo en curso y sus variables.
func (sm *Manager) Set(sessionID, goal, status string, variables map[string]string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	varsBytes, _ := json.Marshal(variables)
	query := `
	INSERT INTO scratchpad (session_id, goal, status, variables, updated_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(session_id) DO UPDATE SET
		goal = excluded.goal,
		status = excluded.status,
		variables = excluded.variables,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := sm.db.Exec(query, sessionID, goal, status, string(varsBytes))
	return err
}

// Clear borra el objetivo activo cuando la tarea fue completada.
func (sm *Manager) Clear(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	_, err := sm.db.Exec(`DELETE FROM scratchpad WHERE session_id = ?;`, sessionID)
	return err
}
