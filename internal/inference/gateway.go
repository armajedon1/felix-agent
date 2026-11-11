package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Gateway implementa un proxy inverso compatible con la API de OpenAI (chat/completions y models).
// Permite balanceo, rotacion automatica de claves ante limites de cuota (429) y enrutamiento hacia cualquier proveedor.
type Gateway struct {
	mu         sync.RWMutex
	targetURL  string
	model      string
	keyPool    *KeyPool
	httpClient *http.Client
}

// NewGateway inicializa la puerta de enlace de inferencia integrada.
func NewGateway(targetBaseURL, defaultModel string, kp *KeyPool) *Gateway {
	if kp == nil {
		kp = NewKeyPool()
	}
	return &Gateway{
		targetURL: strings.TrimRight(targetBaseURL, "/"),
		model:     defaultModel,
		keyPool:   kp,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// UpdateTarget actualiza en caliente el destino y modelo de inferencia.
func (g *Gateway) UpdateTarget(targetBaseURL, model string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if targetBaseURL != "" {
		g.targetURL = strings.TrimRight(targetBaseURL, "/")
	}
	if model != "" {
		g.model = model
	}
}

// GetKeyPool expone el pool de claves administrado por el gateway.
func (g *Gateway) GetKeyPool() *KeyPool {
	return g.keyPool
}

// HandleChatCompletions atiende solicitudes POST /v1/chat/completions con rotacion automatica de claves.
func (g *Gateway) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Metodo no permitido. Utilice POST", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error leyendo cuerpo de solicitud: %v", err), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	g.mu.RLock()
	targetBase := g.targetURL
	defaultModel := g.model
	g.mu.RUnlock()

	if targetBase == "" {
		http.Error(w, "Error: Inferencia no configurada en el gateway. Configure el proveedor mediante WhatsApp o /config.", http.StatusServiceUnavailable)
		return
	}

	upstreamURL := targetBase
	if !strings.HasSuffix(upstreamURL, "/chat/completions") {
		upstreamURL = strings.TrimRight(upstreamURL, "/") + "/chat/completions"
	}

	// Normalizar modelo si viene vacio en la solicitud
	var payload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payload); err == nil {
		if m, ok := payload["model"].(string); !ok || m == "" {
			if defaultModel != "" {
				payload["model"] = defaultModel
				if modBytes, err := json.Marshal(payload); err == nil {
					bodyBytes = modBytes
				}
			}
		}
	}

	totalKeys := g.keyPool.Count()
	maxAttempts := 1
	if totalKeys > 1 {
		maxAttempts = totalKeys
	}

	var lastRespStatus int
	var lastRespBytes []byte
	var lastHeader http.Header

	for attempt := 0; attempt < maxAttempts; attempt++ {
		apiKey := g.keyPool.GetCurrentKey()
		if apiKey == "" {
			// Si el pool no tiene clave pero el cliente envio Authorization, usar la del cliente
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		reqUpstream, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
		if err != nil {
			cancel()
			http.Error(w, fmt.Sprintf("Error creando peticion upstream: %v", err), http.StatusInternalServerError)
			return
		}

		reqUpstream.Header.Set("Content-Type", "application/json")
		reqUpstream.Header.Set("User-Agent", "Felix-Inference-Gateway/1.0")
		if apiKey != "" {
			reqUpstream.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := g.httpClient.Do(reqUpstream)
		if err != nil {
			cancel()
			log.Printf("[Inference Gateway Error] Intento %d/%d fallo conectando a %s: %v", attempt+1, maxAttempts, upstreamURL, err)
			if attempt < maxAttempts-1 {
				g.keyPool.Rotate()
				time.Sleep(300 * time.Millisecond)
				continue
			}
			http.Error(w, fmt.Sprintf("Error comunicando con upstream de inferencia: %v", err), http.StatusBadGateway)
			return
		}

		respBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()

		lastRespStatus = resp.StatusCode
		lastRespBytes = respBytes
		lastHeader = resp.Header

		// Si el proveedor responde 429 (Rate Limit) o cuota agotada y tenemos mas claves, rotar y reintentar
		if resp.StatusCode == http.StatusTooManyRequests || strings.Contains(string(respBytes), "RESOURCE_EXHAUSTED") {
			if totalKeys > 1 && attempt < maxAttempts-1 {
				nextKey := g.keyPool.Rotate()
				log.Printf("[Inference Gateway] Cuota agotada (429). Rotando a la siguiente clave de API (intento %d/%d, clave rotada)...", attempt+1, maxAttempts)
				_ = nextKey
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}

		// Si fue exitoso o es un error no recuperable de cliente (400, etc.), retornar inmediatamente
		for k, v := range lastHeader {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
		w.WriteHeader(lastRespStatus)
		_, _ = w.Write(lastRespBytes)
		return
	}

	// Si se agotaron los intentos, devolver la ultima respuesta recibida
	for k, v := range lastHeader {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(lastRespStatus)
	_, _ = w.Write(lastRespBytes)
}

// HandleModels atiende GET /v1/models para descubrimiento de modelos disponibles.
func (g *Gateway) HandleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Metodo no permitido. Utilice GET", http.StatusMethodNotAllowed)
		return
	}

	g.mu.RLock()
	currentModel := g.model
	if currentModel == "" {
		currentModel = "gemini-2.5-flash"
	}
	g.mu.RUnlock()

	resp := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":         currentModel,
				"object":     "model",
				"created":    time.Now().Unix(),
				"owned_by":   "felix-gateway",
				"permission": []interface{}{},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
