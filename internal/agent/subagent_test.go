package agent

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSubagentManager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_subagent_*")
	if err != nil {
		t.Fatalf("Error creando dir temporal: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_subagent.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Error abriendo base SQLite: %v", err)
	}
	defer db.Close()

	var sentMessages []string
	var mu sync.Mutex
	sendFunc := func(chatJID, message string) error {
		mu.Lock()
		defer mu.Unlock()
		sentMessages = append(sentMessages, message)
		return nil
	}

	sm, err := NewSubagentManager(db, nil, sendFunc, "test-target@lid")
	if err != nil {
		t.Fatalf("Error creando SubagentManager: %v", err)
	}
	sm.SetReportsDir(filepath.Join(tempDir, "reports"))

	// 1. Verificar listado inicial vacio
	initialList, err := sm.List(10)
	if err != nil {
		t.Fatalf("List fallo: %v", err)
	}
	if len(initialList) != 0 {
		t.Errorf("Se esperaba lista vacia, se obtuvieron: %d", len(initialList))
	}

	// 2. Verificar Spawn directo con persistencia
	taskID, err := sm.Spawn("Diagnostico Red", "Escanear puertos de homelab y verificar Tailscale", 5)
	if err != nil {
		t.Fatalf("Spawn fallo: %v", err)
	}
	if taskID == "" {
		t.Fatalf("TaskID no deberia ser vacio")
	}

	// 3. Verificar que aparece en List
	time.Sleep(50 * time.Millisecond)
	list, err := sm.List(10)
	if err != nil {
		t.Fatalf("List fallo tras spawn: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Se esperaba 1 tarea en la lista, se encontraron: %d", len(list))
	}

	task := list[0]
	if task.ID != taskID {
		t.Errorf("ID esperado %s, obtenido %s", taskID, task.ID)
	}
	if task.Title != "Diagnostico Red" {
		t.Errorf("Titulo esperado 'Diagnostico Red', obtenido '%s'", task.Title)
	}

	// 4. Verificar cancelacion
	err = sm.Cancel(taskID)
	if err != nil {
		t.Fatalf("Cancel fallo: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	listAfterCancel, err := sm.List(10)
	if err != nil {
		t.Fatalf("List fallo: %v", err)
	}
	if listAfterCancel[0].Status != "cancelled" {
		t.Errorf("Estado esperado 'cancelled', obtenido '%s'", listAfterCancel[0].Status)
	}

	// 5. Verificar Spawn con Rol de Investigador
	taskID2, err := sm.SpawnWithRole("Auditoria Seguridad", "Verificar permisos de llaves SSH", "auditor", 3)
	if err != nil {
		t.Fatalf("SpawnWithRole fallo: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	rep, err := sm.GetTaskReport(taskID2)
	if err != nil {
		t.Fatalf("GetTaskReport fallo: %v", err)
	}
	if !strings.Contains(rep, "Auditoria Seguridad") {
		t.Errorf("Se esperaba que el reporte contenga el titulo, obtenido: %s", rep)
	}
}
