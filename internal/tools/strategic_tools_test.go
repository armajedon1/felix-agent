package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGitTool_Status(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_git_*")
	if err != nil {
		t.Fatalf("error creando dir temp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gitTool := &GitTool{DefaultRepoDir: tempDir}
	args, _ := json.Marshal(map[string]any{
		"action":    "status",
		"repo_path": tempDir,
	})

	out, err := gitTool.Execute(context.Background(), args)
	if err == nil {
		t.Logf("Git status ejecutado: %s", out)
	}
}

func TestSQLiteInspectorTool(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_sqlite_inspector_*")
	if err != nil {
		t.Fatalf("error creando dir temp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("error creando sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO items (name) VALUES ('item1'), ('item2');
		CREATE INDEX idx_items_name ON items(name);
	`)
	db.Close()
	if err != nil {
		t.Fatalf("error poblando sqlite: %v", err)
	}

	tool := &SQLiteInspectorTool{}

	// 1. Probar tables
	args, _ := json.Marshal(map[string]any{
		"action":  "tables",
		"db_path": dbPath,
	})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("tables fallo: %v", err)
	}
	if !strings.Contains(out, "items") {
		t.Errorf("se esperaba encontrar tabla 'items', obtenido: %s", out)
	}

	// 2. Probar row_counts
	args, _ = json.Marshal(map[string]any{
		"action":  "row_counts",
		"db_path": dbPath,
	})
	out, err = tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("row_counts fallo: %v", err)
	}
	if !strings.Contains(out, "2 filas") {
		t.Errorf("se esperaban 2 filas, obtenido: %s", out)
	}

	// 3. Probar indices
	args, _ = json.Marshal(map[string]any{
		"action":  "indices",
		"db_path": dbPath,
	})
	out, err = tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("indices fallo: %v", err)
	}
	if !strings.Contains(out, "idx_items_name") {
		t.Errorf("se esperaba encontrar indice 'idx_items_name', obtenido: %s", out)
	}

	// 4. Probar analyze
	args, _ = json.Marshal(map[string]any{
		"action":  "analyze",
		"db_path": dbPath,
	})
	out, err = tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("analyze fallo: %v", err)
	}
	if !strings.Contains(out, "ANALISIS DE SALUD") {
		t.Errorf("se esperaba analisis de salud, obtenido: %s", out)
	}
}
