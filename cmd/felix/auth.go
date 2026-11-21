package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"felix-go/internal/config"
)

func VerifyInference(apiBase, apiKey, model string) (int64, error) {
	if apiBase == "" {
		return 0, fmt.Errorf("api_base no configurada")
	}

	endpoint := strings.TrimRight(apiBase, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint = endpoint + "/chat/completions"
		} else if strings.HasSuffix(endpoint, "/openai") {
			endpoint = endpoint + "/chat/completions"
		} else {
			endpoint = endpoint + "/chat/completions"
		}
	}

	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 5,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("fallo conexion: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()
	if resp.StatusCode >= 400 {
		return latency, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return latency, nil
}

func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return fmt.Errorf("sistema operativo no soportado para xdg-open")
	}
	return cmd.Start()
}

func RunAuthManager(configPath string) {
	PrintMOTD()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	fmt.Println("ESTADO DE AUTENTICACION Y PROVEEDOR LLM")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("  Proveedor configurado : %s\n", cfg.Provider)
	fmt.Printf("  Direccion API Base     : %s\n", cfg.APIBase)
	fmt.Printf("  Modelo activo          : %s\n", cfg.Model)
	if cfg.APIKey != "" {
		masked := cfg.APIKey
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("  Clave API / Token      : %s\n", masked)
	} else {
		fmt.Printf("  Clave API / Token      : (Sin clave requerida - Proxy OAuth)\n")
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("INICIO DE SESION OAUTH (SIN API KEYS)")
	fmt.Println("Antigravity Tools gestiona cuentas de Google y OpenAI con rotacion automatica.")
	fmt.Println()
	fmt.Println("Link de inicio de sesion y administracion de cuentas:")
	fmt.Println("  >>> http://127.0.0.1:8045 <<<")
	fmt.Println("  (En red Homelab / Tailscale: http://100.96.230.125:8045)")
	fmt.Println()

	fmt.Println("Comprobando conexion de inferencia actual...")
	latency, errTest := VerifyInference(cfg.APIBase, cfg.APIKey, cfg.Model)
	if errTest == nil {
		fmt.Printf("[OK] Inferencia operativa. Latencia: %d ms\n", latency)
	} else {
		fmt.Printf("[AVISO] No se pudo verificar la conexion: %v\n", errTest)
		fmt.Println("Si utilizas Antigravity Tools, asegurate de abrir el link e iniciar sesion:")
		fmt.Println("  http://127.0.0.1:8045")
	}
	fmt.Println("================================================================================")
}
