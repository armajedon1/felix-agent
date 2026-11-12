package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"felix-go/internal/graph"
)

// SQLiteStore implementa la persistencia de checkpoints sobre SQLite con WAL mode.
type SQLiteStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteStore inicializa la base de datos de checkpoints y aplica migraciones.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("error creando directorio para checkpoints: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error abriendo sqlite checkpoints: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite opera de forma optima con un unico escritor serializado

	s := &SQLiteStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

func (s *SQLiteStore) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS graph_runs (
			run_id TEXT PRIMARY KEY,
			graph_name TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS graph_checkpoints (
			checkpoint_id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			state_payload TEXT NOT NULL,
			step_number INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(run_id) REFERENCES graph_runs(run_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_checkpoints_run ON graph_checkpoints(run_id, step_number);`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("error aplicando esquema de checkpoints: %w", err)
		}
	}

	return nil
}

// CreateRun registra una nueva corrida en estado 'running'.
func (s *SQLiteStore) CreateRun(ctx context.Context, runID, graphName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO graph_runs (run_id, graph_name, status, created_at, updated_at) 
	          VALUES (?, ?, 'running', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	          ON CONFLICT(run_id) DO UPDATE SET updated_at=CURRENT_TIMESTAMP;`
	_, err := s.db.ExecContext(ctx, query, runID, graphName)
	if err != nil {
		return fmt.Errorf("error creando registro de run: %w", err)
	}
	return nil
}

// UpdateRunStatus actualiza el estado de una corrida (running, suspended, completed, failed).
func (s *SQLiteStore) UpdateRunStatus(ctx context.Context, runID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `UPDATE graph_runs SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE run_id = ?;`
	_, err := s.db.ExecContext(ctx, query, status, runID)
	if err != nil {
		return fmt.Errorf("error actualizando estado de corrida: %w", err)
	}
	return nil
}

// GetRunStatus obtiene el estado actual de la corrida.
func (s *SQLiteStore) GetRunStatus(ctx context.Context, runID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM graph_runs WHERE run_id = ?;`, runID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error consultando estado de run: %w", err)
	}
	return status, nil
}

// SaveCheckpoint guarda un snapshot atomico del estado serializado.
func (s *SQLiteStore) SaveCheckpoint(ctx context.Context, runID, nodeID string, step int, state *graph.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("error serializando estado a JSON: %w", err)
	}

	checkpointID := fmt.Sprintf("%s_step_%d", runID, step)
	query := `INSERT OR REPLACE INTO graph_checkpoints 
	          (checkpoint_id, run_id, node_id, state_payload, step_number, created_at)
	          VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP);`

	_, err = s.db.ExecContext(ctx, query, checkpointID, runID, nodeID, string(payload), step)
	if err != nil {
		return fmt.Errorf("error insertando checkpoint: %w", err)
	}

	return nil
}

// GetLatestCheckpoint recupera el snapshot mas reciente de un run.
func (s *SQLiteStore) GetLatestCheckpoint(ctx context.Context, runID string) (*graph.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT state_payload FROM graph_checkpoints 
	          WHERE run_id = ? ORDER BY step_number DESC LIMIT 1;`

	var payload string
	err := s.db.QueryRowContext(ctx, query, runID).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error recuperando ultimo checkpoint: %w", err)
	}

	var state graph.State
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil, fmt.Errorf("error deserializando estado de checkpoint: %w", err)
	}

	return &state, nil
}

// GetCheckpointAtStep recupera el snapshot exacto en el paso especificado (Time-Travel).
func (s *SQLiteStore) GetCheckpointAtStep(ctx context.Context, runID string, step int) (*graph.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT state_payload FROM graph_checkpoints 
	          WHERE run_id = ? AND step_number = ? LIMIT 1;`

	var payload string
	err := s.db.QueryRowContext(ctx, query, runID, step).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error recuperando checkpoint en paso %d: %w", step, err)
	}

	var state graph.State
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil, fmt.Errorf("error deserializando estado en paso %d: %w", step, err)
	}

	return &state, nil
}

// ListCheckpoints lista los metadatos de todos los pasos de una corrida.
func (s *SQLiteStore) ListCheckpoints(ctx context.Context, runID string) ([]graph.CheckpointMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT checkpoint_id, run_id, node_id, step_number, created_at 
	          FROM graph_checkpoints WHERE run_id = ? ORDER BY step_number ASC;`

	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("error listando checkpoints: %w", err)
	}
	defer rows.Close()

	var list []graph.CheckpointMetadata
	for rows.Next() {
		var item graph.CheckpointMetadata
		var createdAtStr string
		if err := rows.Scan(&item.CheckpointID, &item.RunID, &item.NodeID, &item.StepNumber, &createdAtStr); err != nil {
			return nil, fmt.Errorf("error escaneando metadata de checkpoint: %w", err)
		}
		if t, err := time.Parse("2006-01-02 15:04:05", createdAtStr); err == nil {
			item.CreatedAt = t
		}
		list = append(list, item)
	}

	return list, nil
}

// Close cierra la conexion con la base de datos de SQLite.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
