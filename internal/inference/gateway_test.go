package inference

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGatewayRotation(t *testing.T) {
	var callCount int32
	var usedKeys []string

	// Mock server that returns 429 on first key and 200 on second key
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		auth := r.Header.Get("Authorization")
		usedKeys = append(usedKeys, auth)

		if current == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","code":429}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"Respuesta exitosa"}}]}`))
	}))
	defer server.Close()

	kp := NewKeyPool("key-alpha-1234", "key-beta-5678")
	gw := NewGateway(server.URL, "gpt-4o", kp)

	reqPayload := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": "hola"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	gw.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("se esperaba status 200 tras rotacion, obtenido %d", rec.Code)
	}

	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("se esperaban 2 llamadas por reintento, obtenidas %d", callCount)
	}

	// Probar /v1/models
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recModels := httptest.NewRecorder()
	gw.HandleModels(recModels, reqModels)

	if recModels.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 en /v1/models, obtenido %d", recModels.Code)
	}
}
