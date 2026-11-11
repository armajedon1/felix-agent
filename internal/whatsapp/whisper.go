package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

// WhisperClient maneja transcripciones con alta disponibilidad (Groq primario, OpenAI fallback).
type WhisperClient struct {
	groqKey    string
	openAIKey  string
	httpClient *http.Client
}

func NewWhisperClient(groqKey, openAIKey string) *WhisperClient {
	if groqKey == "" {
		groqKey = os.Getenv("GROQ_API_KEY")
	}
	if openAIKey == "" {
		openAIKey = os.Getenv("OPENAI_API_KEY")
	}
	return &WhisperClient{
		groqKey:    groqKey,
		openAIKey:  openAIKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Transcribe envia audio .ogg al proveedor primario (Groq) con failover automatico a OpenAI.
func (w *WhisperClient) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	if len(audioData) == 0 {
		return "", errors.New("audio vacio")
	}

	var lastErr error

	// 1. Intento primario con Groq Whisper
	if w.groqKey != "" {
		text, err := w.callWhisperAPI(ctx, "https://api.groq.com/openai/v1/audio/transcriptions", w.groqKey, "whisper-large-v3-turbo", audioData)
		if err == nil {
			return text, nil
		}
		lastErr = err
		log.Printf("[Whisper Failover] Falla en proveedor primario Groq (%v). Conmutando a OpenAI...", err)
	}

	// 2. Conmutacion a proveedor de respaldo OpenAI Whisper
	if w.openAIKey != "" {
		log.Println("[Whisper Failover] Invocando OpenAI Whisper (whisper-1)...")
		text, err := w.callWhisperAPI(ctx, "https://api.openai.com/v1/audio/transcriptions", w.openAIKey, "whisper-1", audioData)
		if err == nil {
			log.Println("[Whisper Failover] Transcripcion exitosa obtenida desde OpenAI.")
			return text, nil
		}
		lastErr = fmt.Errorf("fallo OpenAI (%w) tras falla de Groq (%v)", err, lastErr)
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("no hay credenciales configuradas para transcripcion de audio (GROQ_API_KEY o OPENAI_API_KEY)")
}

func (w *WhisperClient) callWhisperAPI(ctx context.Context, endpoint, apiKey, model string, audioData []byte) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return "", err
	}

	_ = writer.WriteField("model", model)
	_ = writer.WriteField("language", "es")
	_ = writer.WriteField("response_format", "json")

	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API error %d desde %s: %s", resp.StatusCode, endpoint, string(respBytes))
	}

	var res struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return "", err
	}

	return strings.TrimSpace(res.Text), nil
}
