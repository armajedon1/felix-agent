package agent

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"felix-go/internal/config"
	"felix-go/internal/tools"

	_ "modernc.org/sqlite"
)

func TestContextAssembler(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_hist.db")
	histMgr, err := NewHistoryManager(dbPath, 30)
	if err != nil {
		t.Fatalf("Error creando history manager: %v", err)
	}
	defer histMgr.Close()

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Error abriendo db para scratchpad: %v", err)
	}
	defer rawDB.Close()

	spMgr, err := tools.NewScratchpadManager(rawDB)
	if err != nil {
		t.Fatalf("Error creando scratchpad manager: %v", err)
	}

	cfg := &config.Config{
		SystemPrompt: "Eres Felix, agente de desarrollo.",
	}

	assembler := NewContextAssembler(cfg, histMgr, spMgr)

	sessionID := "sess-context-test"
	histMgr.Append(sessionID, OpenAIMessage{Role: "user", Content: "mensaje previo 1"})
	histMgr.Append(sessionID, OpenAIMessage{Role: "assistant", Content: "respuesta previa 1"})

	err = spMgr.Set(sessionID, "Refactorizar modulo de red", "in_progress", map[string]string{
		"target": "net_tools.go",
	})
	if err != nil {
		t.Fatalf("Error configurando scratchpad: %v", err)
	}

	conv, goal, err := assembler.Assemble(context.Background(), sessionID, "mostrar estado", "CONTEXTO EXTRA")
	if err != nil {
		t.Fatalf("Assemble fallo: %v", err)
	}

	if goal != "Refactorizar modulo de red" {
		t.Errorf("Objetivo activo incorrecto: %s", goal)
	}

	if len(conv) < 4 {
		t.Fatalf("Se esperaban al menos 4 mensajes ensamblados, obtenidos: %d", len(conv))
	}

	// 1. Mensaje de sistema con contexto extra
	if conv[0].Role != "system" || !strings.Contains(conv[0].Content, "CONTEXTO EXTRA") {
		t.Errorf("Mensaje 0 incorrecto: %+v", conv[0])
	}

	// 2. Historial existente
	if conv[1].Role != "user" || conv[1].Content != "mensaje previo 1" {
		t.Errorf("Mensaje 1 incorrecto: %+v", conv[1])
	}
	if conv[2].Role != "assistant" || conv[2].Content != "respuesta previa 1" {
		t.Errorf("Mensaje 2 incorrecto: %+v", conv[2])
	}

	// 3. Entrada actual del usuario
	if conv[3].Role != "user" || conv[3].Content != "mostrar estado" {
		t.Errorf("Mensaje 3 incorrecto: %+v", conv[3])
	}

	// 4. Scratchpad block presente
	foundScratchpad := false
	for _, m := range conv {
		if m.Role == "system" && strings.Contains(m.Content, "SCRATCHPAD") {
			foundScratchpad = true
			if !strings.Contains(m.Content, "net_tools.go") {
				t.Errorf("Variables tecnicas no presentes en scratchpad: %s", m.Content)
			}
			break
		}
	}
	if !foundScratchpad {
		t.Errorf("Bloque de scratchpad no encontrado en la conversacion")
	}
}
