package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"felix-go/internal/agent"
	"felix-go/internal/botfactory"
	"felix-go/internal/config"
	"felix-go/internal/tools"
)

func TestHealthServer(t *testing.T) {
	cfg := &config.Config{
		APIBase: "http://127.0.0.1:8045/v1",
		Model:   "gemini-3.8-flash",
	}
	reg := tools.NewRegistry()
	eng := agent.NewEngine(cfg, reg, nil)

	srv := NewServer("127.0.0.1:0", eng, nil, nil, nil)

	// 1. Probar /healthz
	reqHealthz := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recHealthz := httptest.NewRecorder()
	srv.handleHealthz(recHealthz, reqHealthz)

	if recHealthz.Code != http.StatusOK {
		t.Fatalf("se esperaba codigo 200 en /healthz, obtenido %d", recHealthz.Code)
	}

	var statusResp StatusResponse
	if err := json.Unmarshal(recHealthz.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("error deserializando JSON de healthz: %v", err)
	}

	if statusResp.Status != "healthy" {
		t.Errorf("se esperaba status healthy, obtenido %s", statusResp.Status)
	}

	// 2. Probar /metrics
	reqMetrics := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recMetrics := httptest.NewRecorder()
	srv.handleMetrics(recMetrics, reqMetrics)

	if recMetrics.Code != http.StatusOK {
		t.Fatalf("se esperaba codigo 200 en /metrics, obtenido %d", recMetrics.Code)
	}

	metricsBody := recMetrics.Body.String()
	if !strings.Contains(metricsBody, "felix_uptime_seconds") || !strings.Contains(metricsBody, "felix_llm_primary_healthy") {
		t.Errorf("metricas incompletas: %s", metricsBody)
	}

	// 3. Probar /api/notify validaciones
	reqNotifyGet := httptest.NewRequest(http.MethodGet, "/api/notify", nil)
	reqNotifyGet.RemoteAddr = "127.0.0.1:54321"
	recNotifyGet := httptest.NewRecorder()
	srv.handleNotify(recNotifyGet, reqNotifyGet)
	if recNotifyGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("se esperaba 405 en GET /api/notify, obtenido %d", recNotifyGet.Code)
	}

	reqNotifyBad := httptest.NewRequest(http.MethodPost, "/api/notify", strings.NewReader(`{"message": ""}`))
	reqNotifyBad.RemoteAddr = "127.0.0.1:54321"
	recNotifyBad := httptest.NewRecorder()
	srv.handleNotify(recNotifyBad, reqNotifyBad)
	if recNotifyBad.Code != http.StatusBadRequest {
		t.Errorf("se esperaba 400 en mensaje vacio, obtenido %d", recNotifyBad.Code)
	}

	// 4. Probar IP no autorizada en /api/notify
	reqNotifyForbidden := httptest.NewRequest(http.MethodPost, "/api/notify", strings.NewReader(`{"message": "hola"}`))
	reqNotifyForbidden.RemoteAddr = "203.0.113.19:54321"
	recNotifyForbidden := httptest.NewRecorder()
	srv.handleNotify(recNotifyForbidden, reqNotifyForbidden)
	if recNotifyForbidden.Code != http.StatusForbidden {
		t.Errorf("se esperaba 403 en IP no autorizada, obtenido %d", recNotifyForbidden.Code)
	}
}

type mockBotCompleter struct {
	reply string
}

func (m *mockBotCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return m.reply, nil
}

func TestHealthServerBots(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "health_bots_test.db")

	store, err := botfactory.NewStorage(dbPath)
	if err != nil {
		t.Fatalf("error creando storage: %v", err)
	}
	defer store.Close()

	completer := &mockBotCompleter{reply: "Bienvenido a Pizzeria Test!"}
	bm := botfactory.NewManager(store, completer)

	srv := NewServer("127.0.0.1:0", nil, nil, nil, nil)
	srv.SetBotManager(bm)

	// 1. GET /api/bots (inicialmente vacio)
	reqList := httptest.NewRequest(http.MethodGet, "/api/bots", nil)
	recList := httptest.NewRecorder()
	srv.handleBots(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en GET /api/bots, obtenido %d", recList.Code)
	}

	// 2. POST /api/bots crear desde template
	createBody := `{"template": "pizzeria", "bot_id": "pizzeria-test", "name": "Pizzeria Test"}`
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/bots", strings.NewReader(createBody))
	recCreate := httptest.NewRecorder()
	srv.handleBots(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("se esperaba 201 en POST /api/bots, obtenido %d: %s", recCreate.Code, recCreate.Body.String())
	}

	// 3. GET /api/bots/pizzeria-test
	reqGet := httptest.NewRequest(http.MethodGet, "/api/bots/pizzeria-test", nil)
	recGet := httptest.NewRecorder()
	srv.handleBotRoutes(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en GET /api/bots/pizzeria-test, obtenido %d", recGet.Code)
	}

	// 4. POST /api/bots/pizzeria-test/chat
	chatBody := `{"session_id": "user-123", "message": "Hola que pizzas tienen?"}`
	reqChat := httptest.NewRequest(http.MethodPost, "/api/bots/pizzeria-test/chat", strings.NewReader(chatBody))
	recChat := httptest.NewRecorder()
	srv.handleBotRoutes(recChat, reqChat)
	if recChat.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en chat, obtenido %d: %s", recChat.Code, recChat.Body.String())
	}
	if !strings.Contains(recChat.Body.String(), "Bienvenido a Pizzeria Test!") {
		t.Errorf("respuesta de chat no contiene el mock: %s", recChat.Body.String())
	}

	// 5. GET /api/bots/pizzeria-test/orders
	reqOrders := httptest.NewRequest(http.MethodGet, "/api/bots/pizzeria-test/orders", nil)
	recOrders := httptest.NewRecorder()
	srv.handleBotRoutes(recOrders, reqOrders)
	if recOrders.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en orders, obtenido %d", recOrders.Code)
	}

	// 6. DELETE /api/bots/pizzeria-test
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/bots/pizzeria-test", nil)
	recDel := httptest.NewRecorder()
	srv.handleBotRoutes(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en delete bot, obtenido %d", recDel.Code)
	}
}
