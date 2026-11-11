package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImageGenerateTool genera imagenes con DALL-E 3 mediante la API oficial de OpenAI y las envia al usuario por WhatsApp.
type ImageGenerateTool struct {
	OpenAIKey    string
	DataDir      string
	SendFileFunc func(ctx context.Context, targetJID, filePath, caption string) error
	DefaultJID   string
}

func (t *ImageGenerateTool) Name() string { return "image_generate" }
func (t *ImageGenerateTool) Description() string {
	return "Genera y dibuja una imagen digital a partir de una descripcion visual detallada utilizando DALL-E 3. La imagen generada se guarda localmente en el servidor y se despacha de forma automatica al chat de WhatsApp."
}

func (t *ImageGenerateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Descripcion detallada y visual de la imagen a generar (estilo, composicion, colores, iluminacion, detalles).",
			},
			"size": map[string]any{
				"type":        "string",
				"enum":        []string{"1024x1024", "1024x1792", "1792x1024"},
				"description": "Dimensiones de la imagen: '1024x1024' (cuadrada, por defecto), '1792x1024' (apaisada/horizontal), '1024x1792' (vertical).",
			},
			"quality": map[string]any{
				"type":        "string",
				"enum":        []string{"standard", "hd"},
				"description": "Calidad de renderizado. Por defecto 'standard'.",
			},
			"target_jid": map[string]any{
				"type":        "string",
				"description": "JID de WhatsApp a donde enviar la imagen generada. Si se omite, se envia al destinatario activo.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ImageGenerateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	key := strings.TrimSpace(t.OpenAIKey)
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		return "Error: no hay OPENAI_API_KEY configurada para generacion de imagenes.", nil
	}

	var params struct {
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		Quality   string `json:"quality"`
		TargetJID string `json:"target_jid"`
	}

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return "El prompt para generar la imagen no puede estar vacio.", nil
	}

	size := params.Size
	if size == "" {
		size = "1024x1024"
	}
	quality := params.Quality
	if quality == "" {
		quality = "standard"
	}

	reqBody := map[string]any{
		"model":           "dall-e-3",
		"prompt":          prompt,
		"n":               1,
		"size":            size,
		"quality":         quality,
		"response_format": "b64_json",
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("error serializando request DALL-E: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("error creando request HTTP: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Sprintf("Error comunicando con la API de OpenAI DALL-E: %v", err), nil
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error leyendo respuesta de OpenAI: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Error API OpenAI DALL-E (HTTP %d): %s", resp.StatusCode, string(respBytes)), nil
	}

	var apiResp struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBytes, &apiResp); err != nil || len(apiResp.Data) == 0 {
		return fmt.Sprintf("Respuesta inesperada de OpenAI DALL-E: %s", string(respBytes)), nil
	}

	imgData, err := base64.StdEncoding.DecodeString(apiResp.Data[0].B64JSON)
	if err != nil {
		return "", fmt.Errorf("error decodificando imagen base64: %w", err)
	}

	downloadsDir := filepath.Join(t.DataDir, "downloads")
	_ = os.MkdirAll(downloadsDir, 0755)
	fileName := fmt.Sprintf("img_gen_%d.png", time.Now().Unix())
	targetPath := filepath.Join(downloadsDir, fileName)

	if err := os.WriteFile(targetPath, imgData, 0644); err != nil {
		return "", fmt.Errorf("error guardando imagen en disco: %w", err)
	}

	revised := apiResp.Data[0].RevisedPrompt

	// Despachar automaticamente por WhatsApp si hay funcion de envio
	target := params.TargetJID
	if target == "" {
		target = t.DefaultJID
	}
	if target == "" {
		if sender, ok := ctx.Value("sender_jid").(string); ok && sender != "" {
			target = sender
		}
	}

	var sendNotice string
	if t.SendFileFunc != nil {
		caption := "Imagen generada con DALL-E 3: " + prompt
		if revised != "" {
			caption = revised
		}
		if len([]rune(caption)) > 300 {
			caption = string([]rune(caption)[:300]) + "..."
		}
		if err := t.SendFileFunc(ctx, target, targetPath, caption); err != nil {
			sendNotice = fmt.Sprintf(" (Advertencia: no se pudo enviar por WhatsApp a %s: %v)", target, err)
		} else {
			sendNotice = fmt.Sprintf(" y enviada al chat de WhatsApp (%s)", target)
		}
	}

	return fmt.Sprintf("Imagen generada exitosamente (%s, %d bytes)%s.\nArchivo local: %s\nPrompt original: %s\nPrompt optimizado DALL-E: %s",
		size, len(imgData), sendNotice, targetPath, prompt, revised), nil
}
