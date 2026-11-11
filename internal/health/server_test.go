package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"felix-go/internal/agent"
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
