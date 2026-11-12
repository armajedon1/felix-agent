package checkpoint

import (
	"context"
	"path/filepath"
	"testing"

	"felix-go/internal/graph"
)

func TestSQLiteStore_CRUD(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_checkpoints.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("error inicializando SQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID := "run-sqlite-crud"

	if err := store.CreateRun(ctx, runID, "test_graph"); err != nil {
		t.Fatalf("error creando run: %v", err)
	}

	status, err := store.GetRunStatus(ctx, runID)
	if err != nil || status != "running" {
		t.Fatalf("se esperaba status running, obtenido: %s (err: %v)", status, err)
	}

	// Guardar paso 1
	st1 := graph.NewState(runID)
	st1.StepNumber = 1
	st1.CurrentNode = "node1"
	st1.SetVar("progress", "iniciado")
	if err := store.SaveCheckpoint(ctx, runID, "node1", 1, st1); err != nil {
		t.Fatalf("error guardando paso 1: %v", err)
	}

	// Guardar paso 2
	st2 := st1.Clone()
	st2.StepNumber = 2
	st2.CurrentNode = "node2"
	st2.SetVar("progress", "medio")
	if err := store.SaveCheckpoint(ctx, runID, "node2", 2, st2); err != nil {
		t.Fatalf("error guardando paso 2: %v", err)
	}

	// Probar GetLatestCheckpoint
	latest, err := store.GetLatestCheckpoint(ctx, runID)
	if err != nil || latest == nil {
		t.Fatalf("error recuperando ultimo checkpoint: %v", err)
	}
	if latest.StepNumber != 2 || latest.GetString("progress") != "medio" {
		t.Errorf("datos inesperados en ultimo checkpoint: %+v", latest)
	}

	// Probar GetCheckpointAtStep (paso 1)
	step1, err := store.GetCheckpointAtStep(ctx, runID, 1)
	if err != nil || step1 == nil {
		t.Fatalf("error recuperando paso 1: %v", err)
	}
	if step1.StepNumber != 1 || step1.GetString("progress") != "iniciado" {
		t.Errorf("datos inesperados en paso 1: %+v", step1)
	}

	// Listar checkpoints
	list, err := store.ListCheckpoints(ctx, runID)
	if err != nil {
		t.Fatalf("error listando checkpoints: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("se esperaban 2 checkpoints, obtenidos: %d", len(list))
	}

	// Actualizar estado
	if err := store.UpdateRunStatus(ctx, runID, "completed"); err != nil {
		t.Fatalf("error actualizando estado: %v", err)
	}

	finalStatus, _ := store.GetRunStatus(ctx, runID)
	if finalStatus != "completed" {
		t.Errorf("se esperaba completed, obtenido: %s", finalStatus)
	}
}
