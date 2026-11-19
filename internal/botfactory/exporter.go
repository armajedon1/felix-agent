package botfactory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func ExportStandalone(spec *BotSpec, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("error creando directorio destino: %w", err)
	}

	specRaw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando spec: %w", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "spec.json"), specRaw, 0644); err != nil {
		return fmt.Errorf("error escribiendo spec.json: %w", err)
	}

	goModContent := fmt.Sprintf(`module %s

go 1.22

require modernc.org/sqlite v1.33.1
`, sanitizeModuleName(spec.ID))

	if err := os.WriteFile(filepath.Join(targetDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("error escribiendo go.mod: %w", err)
	}

	mainGoContent := generateStandaloneMainGo(spec)
	if err := os.WriteFile(filepath.Join(targetDir, "main.go"), []byte(mainGoContent), 0644); err != nil {
		return fmt.Errorf("error escribiendo main.go: %w", err)
	}

	readmeContent := fmt.Sprintf(`# %s - Microservicio Autonomo

Bot generado automaticamente por Felix Bot Engine.
Tipo: %s

## Compilacion y Ejecucion

1. Instalar dependencias:
   go mod tidy

2. Compilar binario:
   go build -o bot .

3. Variables de entorno opcionales:
   - PORT: Puerto de escucha (por defecto 8080)
   - LLM_API_BASE: Endpoint compatible con OpenAI (por defecto http://127.0.0.1:8045/v1)
   - LLM_API_KEY: Clave de API
   - LLM_MODEL: Modelo a utilizar (por defecto gemini-2.5-flash)

4. Ejecutar:
   ./bot

## Endpoints HTTP

- POST /api/chat
  Body: {"session_id": "cliente-1", "message": "Hola, que pizzas tienen?"}

- GET /api/orders
  Retorna la lista de pedidos confirmados en formato JSON.

- GET /api/appointments
  Retorna la lista de turnos agendados en formato JSON.

- GET /healthz
  Estado de salud del microservicio.
`, spec.Name, spec.Type)

	if err := os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		return fmt.Errorf("error escribiendo README.md: %w", err)
	}

	return nil
}

func sanitizeModuleName(id string) string {
	res := ""
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			res += string(ch)
		}
	}
	if res == "" {
		return "bot-service"
	}
	return res
}

func generateStandaloneMainGo(spec *BotSpec) string {
	return `package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type CatalogItem struct {
	ID       string  ` + "`json:\"id\"`" + `
	Name     string  ` + "`json:\"name\"`" + `
	Category string  ` + "`json:\"category\"`" + `
	Price    float64 ` + "`json:\"price\"`" + `
}

type BotSpec struct {
	ID              string        ` + "`json:\"id\"`" + `
	Name            string        ` + "`json:\"name\"`" + `
	Type            string        ` + "`json:\"type\"`" + `
	SystemPrompt    string        ` + "`json:\"system_prompt\"`" + `
	Catalog         []CatalogItem ` + "`json:\"catalog\"`" + `
	StrictGrounding bool          ` + "`json:\"strict_grounding\"`" + `
}

var (
	spec BotSpec
	db   *sql.DB
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	specData, err := os.ReadFile("spec.json")
	if err != nil {
		log.Fatalf("error leyendo spec.json: %v", err)
	}
	if err := json.Unmarshal(specData, &spec); err != nil {
		log.Fatalf("error parseando spec.json: %v", err)
	}

	var dbErr error
	db, dbErr = sql.Open("sqlite", "bot_data.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if dbErr != nil {
		log.Fatalf("error abriendo base de datos: %v", dbErr)
	}
	defer db.Close()

	initSchema()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/chat", handleChat)
	mux.HandleFunc("/api/orders", handleOrders)
	mux.HandleFunc("/api/appointments", handleAppointments)

	log.Printf("Iniciando bot '%s' en el puerto %s...", spec.Name, port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func initSchema() {
	_, _ = db.Exec(` + "`" + `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		role TEXT,
		content TEXT,
		created_at DATETIME
	);
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_name TEXT,
		customer_phone TEXT,
		items TEXT,
		total REAL,
		address TEXT,
		payment_method TEXT,
		created_at DATETIME
	);
	CREATE TABLE IF NOT EXISTS appointments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_name TEXT,
		customer_phone TEXT,
		service TEXT,
		start_time DATETIME,
		created_at DATETIME
	);
	` + "`" + `)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "online",
		"bot":    spec.Name,
		"type":   spec.Type,
	})
}

func handleOrders(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, customer_name, customer_phone, items, total, address, payment_method, created_at FROM orders ORDER BY id DESC LIMIT 50")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var orders []map[string]any
	for rows.Next() {
		var id int64
		var name, phone, items, addr, pay string
		var total float64
		var createdAt time.Time
		_ = rows.Scan(&id, &name, &phone, &items, &total, &addr, &pay, &createdAt)
		orders = append(orders, map[string]any{
			"id":             id,
			"customer_name":  name,
			"customer_phone": phone,
			"items":          items,
			"total":          total,
			"address":        addr,
			"payment_method": pay,
			"created_at":     createdAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(orders)
}

func handleAppointments(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, customer_name, customer_phone, service, start_time, created_at FROM appointments ORDER BY start_time ASC LIMIT 50")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var apps []map[string]any
	for rows.Next() {
		var id int64
		var name, phone, service string
		var startTime, createdAt time.Time
		_ = rows.Scan(&id, &name, &phone, &service, &startTime, &createdAt)
		apps = append(apps, map[string]any{
			"id":             id,
			"customer_name":  name,
			"customer_phone": phone,
			"service":        service,
			"start_time":     startTime,
			"created_at":     createdAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apps)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID string ` + "`json:\"session_id\"`" + `
		Message   string ` + "`json:\"message\"`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON invalido", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default"
	}

	reply, err := runInference(r.Context(), req.SessionID, req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"reply": reply,
	})
}

var actionRegex = regexp.MustCompile(` + "`" + `(?s)` + "```" + `json:action\s*(\{.*?\})\s*` + "```" + "`" + `)

func runInference(ctx context.Context, sessionID, userMsg string) (string, error) {
	apiBase := os.Getenv("LLM_API_BASE")
	if apiBase == "" {
		apiBase = "http://127.0.0.1:8045/v1"
	}
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	systemPrompt := fmt.Sprintf("%s\n\nREGLAS: Solo responde con base en la informacion oficial. Prohibido inventar datos.", spec.SystemPrompt)
	bodyData := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMsg},
		},
	}
	raw, _ := json.Marshal(bodyData)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiBase, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	var respJSON struct {
		Choices []struct {
			Message struct {
				Content string ` + "`json:\"content\"`" + `
			} ` + "`json:\"message\"`" + `
		} ` + "`json:\"choices\"`" + `
	}
	_ = json.Unmarshal(respBytes, &respJSON)

	if len(respJSON.Choices) == 0 {
		return "Disculpa, no pude procesar la respuesta en este momento.", nil
	}

	rawReply := respJSON.Choices[0].Message.Content
	cleanReply := actionRegex.ReplaceAllString(rawReply, "")

	return strings.TrimSpace(cleanReply), nil
}
`
}
