package whatsapp

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"felix-go/internal/config"
	_ "modernc.org/sqlite"
)

func TestHandleSlashCommand_Help(t *testing.T) {
	c := &Client{
		cfg: config.DefaultConfig(),
	}
	handled, reply := c.HandleSlashCommand("test@lid", "test@lid", "/help", true)
	if !handled {
		t.Fatal("Se esperaba que /help fuera procesado")
	}

	expectedSnippets := []string{
		"/status",
		"/heal",
		"/snapshot",
		"/diff",
		"/mc",
		"/pc",
		"/services",
		"/config",
		"/pm2",
		"/tasks",
		"/user",
		"/veto",
		"/llm",
		"/clear",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(reply, snippet) {
			t.Errorf("Se esperaba que /help contuviera %q", snippet)
		}
	}

	// Probar restriccion para no-admin
	handledBlocked, replyBlocked := c.HandleSlashCommand("test@lid", "test@lid", "/pm2", false)
	if !handledBlocked || !strings.Contains(replyBlocked, "Acceso denegado") {
		t.Errorf("Se esperaba bloqueo para comando administrativo de no-admin: %s", replyBlocked)
	}
}

func TestHandleWorkstationCommand_Validation(t *testing.T) {
	c := &Client{
		cfg: &config.Config{
			WorkstationHost: "user@10.0.0.1",
		},
	}

	// Sin argumentos clip
	reply := c.handleWorkstationCommand([]string{"/pc", "clip"}, "/pc clip")
	if !strings.Contains(reply, "Uso:") {
		t.Errorf("Respuesta inesperada para clip sin texto: %s", reply)
	}

	// Comando no reconocido
	replyInvalid := c.handleWorkstationCommand([]string{"/pc", "invalid_cmd"}, "/pc invalid_cmd")
	if !strings.Contains(replyInvalid, "no reconocido") {
		t.Errorf("Respuesta inesperada para subcomando invalido: %s", replyInvalid)
	}
}

func TestHandleConfigCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfg := config.DefaultConfig()

	c := &Client{
		cfg: cfg,
	}

	// Probar /config status
	replyStatus := c.handleConfigCommand([]string{"/config", "status"})
	if !strings.Contains(replyStatus, "ESTADO DE CONFIGURACION") {
		t.Errorf("Respuesta inesperada para /config status: %s", replyStatus)
	}

	// Probar /config apibase
	replyBase := c.handleConfigCommand([]string{"/config", "apibase", "https://api.openai.com/v1"})
	if !strings.Contains(replyBase, "API Base actualizada") {
		t.Errorf("Respuesta inesperada para /config apibase: %s", replyBase)
	}
	if cfg.APIBase != "https://api.openai.com/v1" {
		t.Errorf("APIBase no se actualizo correctamente: %s", cfg.APIBase)
	}

	// Probar /config apikey
	replyKey := c.handleConfigCommand([]string{"/config", "apikey", "sk-1234567890abcdef"})
	if !strings.Contains(replyKey, "guardada con exito") {
		t.Errorf("Respuesta inesperada para /config apikey: %s", replyKey)
	}
	if cfg.APIKey != "sk-1234567890abcdef" {
		t.Errorf("APIKey no se actualizo correctamente: %s", cfg.APIKey)
	}
	_ = cfgPath
}

func TestDatabaseQueryMock(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "pedidos.db")

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("error creando base temporal: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE pedidos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nombre TEXT NOT NULL,
		fecha TEXT NOT NULL,
		total REAL DEFAULT 0,
		estado TEXT DEFAULT 'pendiente'
	);
	INSERT INTO pedidos (id, nombre, fecha, total, estado) VALUES
	(1, 'Cliente Test', '09/11/2026, 12:00:00', 5000.0, 'pendiente'),
	(2, 'Cliente Dos', '09/11/2026, 13:00:00', 12000.0, 'entregado');
	`)
	if err != nil {
		t.Fatalf("error insertando registros de prueba: %v", err)
	}

	rows, err := db.Query("SELECT estado, COUNT(*) FROM pedidos GROUP BY estado ORDER BY COUNT(*) DESC")
	if err != nil {
		t.Fatalf("error consultando estados: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			t.Fatalf("error escaneando estado: %v", err)
		}
		count += c
	}
	if count != 2 {
		t.Errorf("Se esperaban 2 registros en total, obtenidos %d", count)
	}
}

func TestMinecraftCommandValidation(t *testing.T) {
	// Whitelist add sin user
	reply := handleMinecraftCommand([]string{"/mc", "whitelist", "add"})
	if !strings.Contains(reply, "Uso:") {
		t.Errorf("Respuesta inesperada para whitelist add vacio: %s", reply)
	}

	// Whitelist remove sin user
	reply = handleMinecraftCommand([]string{"/mc", "whitelist", "remove"})
	if !strings.Contains(reply, "Uso:") {
		t.Errorf("Respuesta inesperada para whitelist remove vacio: %s", reply)
	}

	// Kick sin user
	reply = handleMinecraftCommand([]string{"/mc", "kick"})
	if !strings.Contains(reply, "Uso:") {
		t.Errorf("Respuesta inesperada para kick vacio: %s", reply)
	}

	// Say sin msg
	reply = handleMinecraftCommand([]string{"/mc", "say"})
	if !strings.Contains(reply, "Uso:") {
		t.Errorf("Respuesta inesperada para say vacio: %s", reply)
	}
}
