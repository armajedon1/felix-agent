package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VisionInspectTool transcribe texto y analiza contenido visual de imagenes con Gemini Vision.
type VisionInspectTool struct {
	APIBase string
	APIKey  string
	Model   string
}

func (t *VisionInspectTool) Name() string { return "vision_inspect" }
func (t *VisionInspectTool) Description() string {
	return "Transcribe texto, notas manuscritas, capturas de pantalla o fotos, y describe el contenido visual de imagenes usando vision multimodal."
}
func (t *VisionInspectTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_path": map[string]any{
				"type":        "string",
				"description": "Ruta absoluta o relativa al archivo de imagen (.jpg, .jpeg, .png, .webp).",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Instruccion especifica para el analisis (ej: 'Transcribe todo el texto exactamente', 'Describe que hay en la foto'). Por defecto transcribe todo el texto visible.",
			},
		},
		"required": []string{"image_path"},
	}
}

func (t *VisionInspectTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ImagePath string `json:"image_path"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.ImagePath == "" {
		return "", errors.New("image_path es obligatorio")
	}

	data, err := os.ReadFile(params.ImagePath)
	if err != nil {
		return "", fmt.Errorf("error leyendo imagen en %s: %w", params.ImagePath, err)
	}

	ext := strings.ToLower(filepath.Ext(params.ImagePath))
	mimeType := "image/jpeg"
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".webp":
		mimeType = "image/webp"
	case ".gif":
		mimeType = "image/gif"
	}

	b64Data := base64.StdEncoding.EncodeToString(data)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, b64Data)

	prompt := params.Prompt
	if prompt == "" {
		prompt = "Transcribe de forma fiel, completa y exacta todo el texto visible en esta imagen (incluyendo notas manuscritas, carteles, documentos o capturas). Si hay elementos visuales relevantes, mencionalos brevemente."
	}

	type contentPart struct {
		Type     string            `json:"type"`
		Text     string            `json:"text,omitempty"`
		ImageURL map[string]string `json:"image_url,omitempty"`
	}

	type visionMessage struct {
		Role    string        `json:"role"`
		Content []contentPart `json:"content"`
	}

	type visionReq struct {
		Model    string          `json:"model"`
		Messages []visionMessage `json:"messages"`
	}

	modelName := t.Model
	if modelName == "" {
		modelName = "gemini-3.8-flash"
	}

	reqBody := visionReq{
		Model: modelName,
		Messages: []visionMessage{
			{
				Role: "user",
				Content: []contentPart{
					{Type: "text", Text: prompt},
					{Type: "image_url", ImageURL: map[string]string{"url": dataURL}},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	apiURL := strings.TrimRight(t.APIBase, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if t.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+t.APIKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("error conectando con servicio de vision: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("error API vision (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return "", fmt.Errorf("error deserializando respuesta vision: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", fmt.Errorf("error devuelto por vision LLM: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("el modelo de vision no retorno contenido")
	}

	res := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if res == "" {
		return "(No se detecto texto legible en la imagen)", nil
	}

	return res, nil
}
