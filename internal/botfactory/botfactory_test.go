package botfactory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type mockCompleter struct {
	response string
}

func (m *mockCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return m.response, nil
}

func TestStorageAndCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	piz := TemplatePizzeria()
	if err := store.SaveBot(piz); err != nil {
		t.Fatalf("error guardando bot: %v", err)
	}

	retrieved, err := store.GetBot(piz.ID)
	if err != nil {
		t.Fatalf("error obteniendo bot: %v", err)
	}
	if retrieved.Name != piz.Name {
		t.Errorf("se esperaba nombre %s, se obtuvo %s", piz.Name, retrieved.Name)
	}

	bots, err := store.ListBots()
	if err != nil || len(bots) != 1 {
		t.Fatalf("se esperaba 1 bot en la lista, se obtuvieron %d", len(bots))
	}

	if err := store.DeleteBot(piz.ID); err != nil {
		t.Fatalf("error eliminando bot: %v", err)
	}

	_, err = store.GetBot(piz.ID)
	if err == nil {
		t.Fatalf("se esperaba error al obtener bot eliminado")
	}
}

func TestOrderTakingDeterministicMath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_orders_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	piz := TemplatePizzeria()
	if err := store.SaveBot(piz); err != nil {
		t.Fatalf("error guardando bot: %v", err)
	}

	mockResp := `Excelente! Tu pedido de 2 pizzas muzzarella y 1 coca cola fue registrado con exito. Llegara a Avenida Libertador 1200 en 35 minutos.
` + "```json:action\n" +
		`{
  "action": "create_order",
  "customer_name": "Carlos Gomez",
  "customer_phone": "+5491100001111",
  "delivery_address": "Avenida Libertador 1200",
  "payment_method": "efectivo",
  "items": [
    {"id": "piz-muz", "qty": 2},
    {"id": "beb-coc", "qty": 1}
  ]
}` + "\n```"

	mgr := NewManager(store, &mockCompleter{response: mockResp})

	result, err := mgr.Chat(context.Background(), piz.ID, "+5491100001111", "Quiero 2 muzzarellas y una coca cola para Carlos Gomez en Avenida Libertador 1200, pago en efectivo")
	if err != nil {
		t.Fatalf("error ejecutando chat: %v", err)
	}

	if result.Order == nil {
		t.Fatalf("se esperaba que se cree una orden confirmada")
	}

	// Calculo determinista: 2 * 8000 + 1 * 3000 = 19000
	expectedTotal := 19000.0
	if result.Order.Total != expectedTotal {
		t.Errorf("se esperaba total $%.2f, pero se calculo $%.2f", expectedTotal, result.Order.Total)
	}

	if len(result.Order.Items) != 2 {
		t.Errorf("se esperaban 2 items, se obtuvieron %d", len(result.Order.Items))
	}

	orders, err := store.ListOrders(piz.ID, 10)
	if err != nil || len(orders) != 1 {
		t.Fatalf("se esperaba 1 orden persistida en sqlite")
	}
	if orders[0].Total != expectedTotal {
		t.Errorf("total en base no coincide: esperado %.2f, obtenido %.2f", expectedTotal, orders[0].Total)
	}
}

func TestAppointmentBookingAndCollision(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_apps_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	apptBot := TemplateAppointments()
	if err := store.SaveBot(apptBot); err != nil {
		t.Fatalf("error guardando bot: %v", err)
	}

	targetTime := "2026-11-20T16:00:00"
	mockResp := `Perfecto Maria, te agende el turno para Corte y Estilismo el 20 de noviembre a las 16:00 hs.
` + "```json:action\n" +
		`{
  "action": "create_appointment",
  "customer_name": "Maria Lopez",
  "customer_phone": "+5491122223333",
  "service": "Corte y Estilismo",
  "date_time": "` + targetTime + `"
}` + "\n```"

	mgr := NewManager(store, &mockCompleter{response: mockResp})

	result, err := mgr.Chat(context.Background(), apptBot.ID, "+5491122223333", "Hola, quiero agendar un corte para el 20 de noviembre a las 16:00")
	if err != nil {
		t.Fatalf("error en chat de turnos: %v", err)
	}

	if result.Appointment == nil {
		t.Fatalf("se esperaba confirmacion de turno")
	}

	apps, err := store.ListAppointments(apptBot.ID, 10)
	if err != nil || len(apps) != 1 {
		t.Fatalf("se esperaba 1 turno en base de datos")
	}

	// Verificar deteccion de colision en el mismo horario
	parsedTarget, _ := time.Parse("2006-01-02T15:04:05", targetTime)
	conflict, err := store.HasAppointmentConflict(apptBot.ID, parsedTarget, 30)
	if err != nil || !conflict {
		t.Errorf("se esperaba que detecte conflicto para el horario ocupado")
	}

	// Intento de agendar en el mismo horario debe advertir ocupado
	result2, err := mgr.Chat(context.Background(), apptBot.ID, "+5491199998888", "Quiero un turno a las 16:00 tambien")
	if err != nil {
		t.Fatalf("error en chat turnos colision: %v", err)
	}
	if result2.Appointment != nil {
		t.Errorf("no deberia haber agendado turno colisionante")
	}
}

func TestTemplateCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_templates_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	mgr := NewManager(store, &mockCompleter{response: "ok"})

	spec, err := mgr.CreateFromTemplate("pizzeria", "mi-pizzeria", "Pizzeria Don Juan", nil)
	if err != nil {
		t.Fatalf("error creando desde plantilla: %v", err)
	}
	if spec.ID != "mi-pizzeria" || spec.Name != "Pizzeria Don Juan" {
		t.Errorf("personalizacion de ID o Name no funciono")
	}

	specFaq, err := mgr.CreateFromTemplate("faq", "faq-soporte-mio", "Soporte Tecnico", nil)
	if err != nil {
		t.Fatalf("error creando plantilla faq: %v", err)
	}
	if specFaq.Type != TypeFAQSupport {
		t.Errorf("tipo de bot incorrecto para faq")
	}

	specMosiah, err := mgr.CreateFromTemplate("mosiah", "mosiah-replica", "Mosiah Barrio Varela", nil)
	if err != nil {
		t.Fatalf("error creando plantilla mosiah: %v", err)
	}
	if specMosiah.Name != "Mosiah Barrio Varela" || len(specMosiah.Nicknames) == 0 {
		t.Errorf("error en plantilla mosiah")
	}
}

func TestReplicateBot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_replicate_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	mgr := NewManager(store, &mockCompleter{response: "ok"})

	piz := TemplatePizzeria()
	if err := store.SaveBot(piz); err != nil {
		t.Fatalf("error guardando bot origen: %v", err)
	}

	replica, err := mgr.ReplicateBot(piz.ID, "pizzeria-roma", "Pizzeria Roma", map[string]any{
		"description": "Replica para sucursal Roma",
	})
	if err != nil {
		t.Fatalf("error replicando bot: %v", err)
	}

	if replica.ID != "pizzeria-roma" || replica.Name != "Pizzeria Roma" || replica.ReplicationSource != piz.ID {
		t.Errorf("datos de replica no coinciden: %+v", replica)
	}

	if replica.Description != "Replica para sucursal Roma" {
		t.Errorf("override no aplicado: %s", replica.Description)
	}
}

func TestExportStandalone(t *testing.T) {
	tmpDir := t.TempDir()
	exportDir := filepath.Join(tmpDir, "exported-bot")

	piz := TemplatePizzeria()
	if err := ExportStandalone(piz, exportDir); err != nil {
		t.Fatalf("error exportando bot: %v", err)
	}

	expectedFiles := []string{"spec.json", "main.go", "go.mod", "README.md"}
	for _, f := range expectedFiles {
		p := filepath.Join(exportDir, f)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("archivo esperado %s no existe", f)
		}
	}
}

func TestBotNumbersCollisionAndLogs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bots_collision_test.db")

	store, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	botA := &BotSpec{
		ID:          "bot-a",
		Name:        "Bot Alpha",
		Type:        TypeOrderTaking,
		PhoneNumber: "+5491100002222",
		Status:      "active",
	}
	if err := store.SaveBot(botA); err != nil {
		t.Fatalf("error guardando bot A: %v", err)
	}

	botB := &BotSpec{
		ID:          "bot-b",
		Name:        "Bot Beta",
		Type:        TypeFAQSupport,
		PhoneNumber: "+5491100002222",
		Status:      "active",
	}
	if err := store.SaveBot(botB); err == nil {
		t.Fatalf("se esperaba error de colision al guardar bot B con mismo numero activo")
	}

	botB.Status = "inactive"
	if err := store.SaveBot(botB); err != nil {
		t.Fatalf("error guardando bot B inactivo: %v", err)
	}

	if err := store.SetBotActive("bot-b", true); err == nil {
		t.Fatalf("se esperaba error al activar bot B mientras bot A usa el mismo numero")
	}

	if err := store.SetBotActive("bot-a", false); err != nil {
		t.Fatalf("error desactivando bot A: %v", err)
	}
	if err := store.SetBotActive("bot-b", true); err != nil {
		t.Fatalf("error activando bot B tras desactivar bot A: %v", err)
	}

	numbers, err := store.ListNumbers()
	if err != nil {
		t.Fatalf("error listando numeros: %v", err)
	}
	if len(numbers) != 2 {
		t.Fatalf("se esperaban 2 asignaciones de numero, se obtuvieron %d", len(numbers))
	}

	log1 := &BotLogEntry{
		BotID:        "bot-b",
		Level:        "INFO",
		Direction:    "INBOUND",
		SenderNumber: "+5491133334444",
		Message:      "Hola, consulta de soporte",
		LatencyMs:    120,
	}
	if err := store.LogEvent(log1); err != nil {
		t.Fatalf("error guardando log: %v", err)
	}

	logs, err := store.GetBotLogs("bot-b", 10, "")
	if err != nil || len(logs) != 1 {
		t.Fatalf("se esperaba 1 log para bot-b, se obtuvieron %d", len(logs))
	}
	if logs[0].Message != "Hola, consulta de soporte" {
		t.Errorf("mensaje de log inesperado: %s", logs[0].Message)
	}
}
