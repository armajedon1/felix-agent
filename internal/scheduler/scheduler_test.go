package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduler_CleanOldDownloads(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_sched_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "reminders.db")
	sched, err := NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("error creando scheduler: %v", err)
	}
	sched.SetDataDir(tmpDir)

	downloadsDir := filepath.Join(tmpDir, "downloads")
	_ = os.MkdirAll(downloadsDir, 0755)

	// Crear archivo reciente
	recentFile := filepath.Join(downloadsDir, "recent.txt")
	_ = os.WriteFile(recentFile, []byte("reciente"), 0644)

	// Crear archivo antiguo
	oldFile := filepath.Join(downloadsDir, "old.txt")
	_ = os.WriteFile(oldFile, []byte("antiguo"), 0644)
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	deleted, err := sched.CleanOldDownloads(7)
	if err != nil {
		t.Fatalf("error limpiando descargas: %v", err)
	}

	if deleted != 1 {
		t.Errorf("se esperaba 1 archivo eliminado, se eliminaron %d", deleted)
	}

	if _, err := os.Stat(recentFile); os.IsNotExist(err) {
		t.Errorf("archivo reciente no debio ser eliminado")
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("archivo antiguo debio ser eliminado")
	}
}

func TestScheduler_OptimizeDatabases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_opt_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "reminders.db")
	sched, err := NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("error creando scheduler: %v", err)
	}
	sched.SetDataDir(tmpDir)

	// Crear un recordatorio para poblar la DB
	loc := time.FixedZone("ART", -3*3600)
	targetTime := time.Now().In(loc).Add(1 * time.Hour)
	if err := sched.AddReminder("test@s.whatsapp.net", "Matias", targetTime, "Aviso de prueba"); err != nil {
		t.Fatalf("error agregando recordatorio: %v", err)
	}

	if err := sched.OptimizeDatabases(); err != nil {
		t.Errorf("OptimizeDatabases devolvio error: %v", err)
	}
}

func TestScheduler_BackupDatabases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_bkp_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "reminders.db")
	sched, err := NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("error creando scheduler: %v", err)
	}
	sched.SetDataDir(tmpDir)

	backedUp, err := sched.BackupDatabases(7)
	if err != nil {
		t.Fatalf("BackupDatabases devolvio error: %v", err)
	}

	if backedUp < 1 {
		t.Errorf("se esperaba al menos 1 base respaldada, obtenido %d", backedUp)
	}

	// Verificar que el archivo en backups existe
	backupsDir := filepath.Join(tmpDir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no se encontraron archivos en backupsDir: %v", err)
	}
}

func TestScheduler_ArchiveOldSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_archive_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "reminders.db")
	sched, err := NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("error creando scheduler: %v", err)
	}
	sched.SetDataDir(tmpDir)

	// Crear sessions.db con mensajes antiguos
	sessionDBPath := filepath.Join(tmpDir, "sessions.db")
	sDB, err := NewScheduler(sessionDBPath)
	if err != nil {
		t.Fatalf("error creando dummy sessions: %v", err)
	}
	_, _ = sDB.db.Exec(`
		CREATE TABLE session_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_calls TEXT DEFAULT '',
			tool_call_id TEXT DEFAULT '',
			created_at DATETIME
		);
		INSERT INTO session_messages (session_id, role, content, created_at)
		VALUES ('chat-1', 'user', 'hola viejo', '2025-01-01 10:00:00'),
		       ('chat-1', 'user', 'hola nuevo', datetime('now'));
	`)
	_ = sDB.db.Close()

	archived, err := sched.ArchiveOldSessions(90)
	if err != nil {
		t.Fatalf("ArchiveOldSessions fallo: %v", err)
	}

	if archived != 1 {
		t.Errorf("se esperaba 1 mensaje archivado, obtenido %d", archived)
	}
}
