package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"felix-go/internal/botfactory"
)

type mockLLMCompleter struct {
	reply string
}

func (m *mockLLMCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return m.reply, nil
}

func TestBotFactoryTool(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bot_tool_test.db")

	store, err := botfactory.NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	completer := &mockLLMCompleter{reply: "Hola! En que puedo ayudarte?"}
	mgr := botfactory.NewManager(store, completer)

	tool := &BotFactoryTool{Manager: mgr}

	if tool.Name() != "bot_factory" {
		t.Errorf("nombre incorrecto de la herramienta: %s", tool.Name())
	}

	// 1. Listar bots inicialmente (debe estar vacio)
	res, err := tool.Execute(context.Background(), []byte(`{"action": "list"}`))
	if err != nil {
		t.Fatalf("error listando: %v", err)
	}
	if !strings.Contains(res, "No hay bots") {
		t.Errorf("respuesta inesperada: %s", res)
	}

	// 2. Crear bot desde plantilla pizzeria
	res, err = tool.Execute(context.Background(), []byte(`{
		"action": "create_from_template",
		"template": "pizzeria",
		"bot_id": "pizzeria-argentina",
		"name": "Pizzeria Argentina"
	}`))
	if err != nil {
		t.Fatalf("error creando bot desde template: %v", err)
	}
	if !strings.Contains(res, "Pizzeria Argentina") {
		t.Errorf("error en respuesta de creacion: %s", res)
	}

	// 3. Obtener bot
	res, err = tool.Execute(context.Background(), []byte(`{
		"action": "get",
		"bot_id": "pizzeria-argentina"
	}`))
	if err != nil {
		t.Fatalf("error obteniendo bot: %v", err)
	}
	if !strings.Contains(res, "pizzeria-argentina") {
		t.Errorf("bot obtenido no contiene ID: %s", res)
	}

	// 4. Chat interactivo
	res, err = tool.Execute(context.Background(), []byte(`{
		"action": "chat",
		"bot_id": "pizzeria-argentina",
		"session_id": "cliente-1",
		"message": "Hola, que menu tienen?"
	}`))
	if err != nil {
		t.Fatalf("error en chat: %v", err)
	}
	if !strings.Contains(res, "Hola! En que puedo ayudarte?") {
		t.Errorf("respuesta de chat no coincide con el mock: %s", res)
	}

	// 5. Exportar microservicio
	exportDir := filepath.Join(tmpDir, "exported-service")
	res, err = tool.Execute(context.Background(), []byte(`{
		"action": "export",
		"bot_id": "pizzeria-argentina",
		"export_dir": "` + exportDir + `"
	}`))
	if err != nil {
		t.Fatalf("error exportando microservicio: %v", err)
	}
	if !strings.Contains(res, "exportado exitosamente") {
		t.Errorf("respuesta de exportacion inesperada: %s", res)
	}

	// 6. Eliminar bot
	res, err = tool.Execute(context.Background(), []byte(`{
		"action": "delete",
		"bot_id": "pizzeria-argentina"
	}`))
	if err != nil {
		t.Fatalf("error eliminando bot: %v", err)
	}
	if !strings.Contains(res, "eliminado correctamente") {
		t.Errorf("respuesta de eliminacion inesperada: %s", res)
	}
}
