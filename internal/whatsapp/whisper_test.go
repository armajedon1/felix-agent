package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhisperClient_Failover(t *testing.T) {
	// 1. Mock de Groq que devuelve error 500
	groqMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal Groq Error"}`))
	}))
	defer groqMock.Close()

	// 2. Mock de OpenAI que devuelve 200 OK con texto
	openAIMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text": "Transcripcion exitosa desde OpenAI Whisper"}`))
	}))
	defer openAIMock.Close()

	client := NewWhisperClient("fake_groq_key", "fake_openai_key")

	// Probar llamada directa al metodo callWhisperAPI para verificar los mocks
	ctx := context.Background()
	_, errGroq := client.callWhisperAPI(ctx, groqMock.URL, "fake_key", "model-test", []byte("fake_audio_bytes"))
	if errGroq == nil {
		t.Errorf("se esperaba error desde groqMock")
	}

	textOpenAI, errOpenAI := client.callWhisperAPI(ctx, openAIMock.URL, "fake_key", "model-test", []byte("fake_audio_bytes"))
	if errOpenAI != nil || textOpenAI != "Transcripcion exitosa desde OpenAI Whisper" {
		t.Errorf("falla en openAIMock: text=%q, err=%v", textOpenAI, errOpenAI)
	}

	// 3. Audio vacio
	_, errEmpty := client.Transcribe(ctx, nil)
	if errEmpty == nil {
		t.Errorf("se esperaba error ante audio vacio")
	}
}
