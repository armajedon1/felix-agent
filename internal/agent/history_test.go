package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_history_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_sessions.db")
	hm, err := NewHistoryManager(dbPath, 10)
	if err != nil {
		t.Fatalf("error inicializando HistoryManager: %v", err)
	}
	defer hm.Close()

	sessionID := "test-session-123"

	// 1. Probar insercion y lectura
	hm.Append(sessionID, OpenAIMessage{Role: "user", Content: "Hola Félix"})
	hm.Append(sessionID, OpenAIMessage{Role: "assistant", Content: "Hola Matías, cómo estás?"})
	hm.Append(sessionID, OpenAIMessage{Role: "user", Content: "Recordá que el servidor usa el puerto 8045"})

	count, err := hm.Count(sessionID)
	if err != nil {
		t.Fatalf("error contando: %v", err)
	}
	if count != 3 {
		t.Errorf("se esperaban 3 mensajes, se obtuvieron %d", count)
	}

	msgs := hm.Get(sessionID)
	if len(msgs) != 3 {
		t.Fatalf("se esperaban 3 mensajes en Get, se obtuvieron %d", len(msgs))
	}
	if msgs[0].Content != "Hola Félix" || msgs[2].Content != "Recordá que el servidor usa el puerto 8045" {
		t.Errorf("orden cronologico incorrecto: %+v", msgs)
	}

	// 2. Probar resumenes y compactacion
	summary := "- El servidor usa el puerto 8045."
	if err := hm.SaveSummary(sessionID, summary, 3); err != nil {
		t.Fatalf("error guardando resumen: %v", err)
	}

	retSummary, untilID, err := hm.GetSummary(sessionID)
	if err != nil {
		t.Fatalf("error obteniendo resumen: %v", err)
	}
	if retSummary != summary || untilID != 3 {
		t.Errorf("resumen no coincide: %s vs %s (untilID: %d)", retSummary, summary, untilID)
	}

	// 3. Probar poda de mensajes antiguos
	if err := hm.DeleteMessagesUpTo(sessionID, 2); err != nil {
		t.Fatalf("error eliminando mensajes hasta ID 2: %v", err)
	}

	remainingCount, _ := hm.Count(sessionID)
	if remainingCount != 1 {
		t.Errorf("se esperaba 1 mensaje remanente, se obtuvieron %d", remainingCount)
	}

	// 4. Probar reseteo total con Clear
	if err := hm.Clear(sessionID); err != nil {
		t.Fatalf("error en Clear: %v", err)
	}
	clearedCount, _ := hm.Count(sessionID)
	if clearedCount != 0 {
		t.Errorf("se esperaban 0 mensajes tras Clear, se obtuvieron %d", clearedCount)
	}
	clearedSummary, _, _ := hm.GetSummary(sessionID)
	if clearedSummary != "" {
		t.Errorf("se esperaba resumen vacio tras Clear, se obtuvo: %s", clearedSummary)
	}
}
