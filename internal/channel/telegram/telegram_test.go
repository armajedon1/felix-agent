package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"felix-go/internal/channel"
)

func TestTelegramChannelMock(t *testing.T) {
	var sentTexts []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/bot12345:TOKEN/getMe" {
			_ = json.NewEncoder(w).Encode(GetMeResponse{
				OK: true,
				Result: struct {
					ID        int64  `json:"id"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				}{
					ID:        999,
					FirstName: "FelixBot",
					Username:  "felix_agent_bot",
				},
			})
			return
		}

		if r.URL.Path == "/bot12345:TOKEN/getUpdates" {
			_ = json.NewEncoder(w).Encode(GetUpdatesResponse{
				OK: true,
				Result: []Update{
					{
						UpdateID: 100,
						Message: &Message{
							MessageID: 1,
							From: &User{
								ID:        123456,
								FirstName: "Admin",
								Username:  "adm_user",
							},
							Chat: Chat{
								ID:   123456,
								Type: "private",
							},
							Date: time.Now().Unix(),
							Text: "hola desde test",
						},
					},
				},
			})
			return
		}

		if r.URL.Path == "/bot12345:TOKEN/sendMessage" {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if txt, ok := payload["text"].(string); ok {
				sentTexts = append(sentTexts, txt)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 2}}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tg := NewTelegramChannel("12345:TOKEN", filepath.Join(tmpDir, "dl"), nil)
	tg.apiBase = server.URL // apuntar mock

	receivedChan := make(chan channel.IncomingMessage, 1)
	handler := func(ctx context.Context, msg channel.IncomingMessage) error {
		receivedChan <- msg
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tg.Start(ctx, handler); err != nil {
		t.Fatalf("error iniciando canal Telegram: %v", err)
	}

	if !tg.IsConnected() {
		t.Errorf("se esperaba que el canal estuviera conectado")
	}

	select {
	case msg := <-receivedChan:
		if msg.Text != "hola desde test" {
			t.Errorf("mensaje inesperado: %s", msg.Text)
		}
		if msg.SenderID != "123456" {
			t.Errorf("sender id inesperado: %s", msg.SenderID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout esperando mensaje entrante de Telegram")
	}

	// Probar envio de mensaje
	err := tg.Send(context.Background(), channel.OutgoingMessage{
		TargetChatID: "123456",
		Text:         "Respuesta de prueba",
	})
	if err != nil {
		t.Fatalf("error enviando mensaje: %v", err)
	}

	if len(sentTexts) != 1 || sentTexts[0] != "Respuesta de prueba" {
		t.Errorf("texto no enviado correctamente: %+v", sentTexts)
	}

	_ = tg.Stop()
	if tg.IsConnected() {
		t.Errorf("se esperaba canal desconectado tras Stop()")
	}
}
