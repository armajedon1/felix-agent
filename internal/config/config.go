package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config define las opciones de configuracion del agente Felix.
type Config struct {
	mu sync.RWMutex `json:"-"`

	OwnerJID          string            `json:"owner_jid"`
	PhoneNumber       string            `json:"phone_number,omitempty"`
	AllowedUsers      []string          `json:"allowed_users"`
	AllowedGroups     []string          `json:"allowed_groups"`
	APIBase           string            `json:"api_base"`
	APIKey            string            `json:"api_key"`
	OpenAIAPIKey      string            `json:"openai_api_key,omitempty"`
	GroqAPIKey        string            `json:"groq_api_key,omitempty"`
	FallbackAPIBase   string            `json:"fallback_api_base,omitempty"`
	FallbackAPIKey    string            `json:"fallback_api_key,omitempty"`
	FallbackModel     string            `json:"fallback_model,omitempty"`
	Model             string            `json:"model"`
	SystemPrompt      string            `json:"system_prompt"`
	WorkspaceDir      string            `json:"workspace_dir"`
	DataDir           string            `json:"data_dir"`
	SkillsDir         string            `json:"skills_dir"`
	HealthPort        string            `json:"health_port"`
	MonitoredServices []string          `json:"monitored_services"`
	WorkstationHost   string            `json:"workstation_host,omitempty"`
	MaxIterations     int               `json:"max_iterations"`
	MCPServers        map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env,omitempty"`
	} `json:"mcp_servers,omitempty"`
}

// DefaultConfig retorna la configuracion base recomendada para una instalacion nueva.
func DefaultConfig() *Config {
	return &Config{
		OwnerJID:          "",
		PhoneNumber:       "",
		AllowedUsers:      []string{},
		AllowedGroups:     []string{},
		APIBase:           "http://127.0.0.1:8045/v1",
		APIKey:            "",
		OpenAIAPIKey:      "",
		GroqAPIKey:        "",
		FallbackAPIBase:   "https://api.groq.com/openai/v1",
		FallbackAPIKey:    "",
		FallbackModel:     "llama-3.3-70b-versatile",
		Model:             "gemini-2.5-flash",
		SystemPrompt: `Eres Felix, un asistente autonomo de ingenieria, DevOps y administracion de sistemas Linux en WhatsApp.
Principios de comportamiento:
1. Utilidad directa: responde de inmediato con datos, comandos, codigo o resultados solicitados, sin rodeos ni formulas de cortesia vacias.
2. Prohibicion estricta de emojis: NUNCA utilices emojis en ningun mensaje, respuesta, log o codigo.
3. Precision tecnica: respuestas sobrias, concisas y de alta precision tecnica.
4. Uso autonomo de herramientas: para consultar el estado del servidor, archivos, procesos o red, ejecuta las herramientas pertinentes en lugar de especular.
5. Seguridad y aislamiento: respeta los limites del sistema operativo y ejecuta cambios con verificacion previa.`,
		WorkspaceDir:      "./workspace",
		DataDir:           "./data",
		SkillsDir:         "./skills",
		HealthPort:        "8055",
		MonitoredServices: []string{},
		MaxIterations:     15,
	}
}

// LoadConfig carga el archivo config.json o crea uno por defecto si no existe.
func LoadConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, err
		}
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, data, 0644); err != nil {
			return nil, fmt.Errorf("error creando config por defecto: %w", err)
		}
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error leyendo config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error deserializando config JSON: %w", err)
	}

	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 15
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = "./workspace"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = "./skills"
	}
	if cfg.HealthPort == "" {
		cfg.HealthPort = "8055"
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-2.5-flash"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = DefaultConfig().SystemPrompt
	}

	return &cfg, nil
}

// Save persiste la configuracion actual en disco de forma segura y atomica.
func (c *Config) Save(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando configuracion: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("error creando directorio de config: %w", err)
		}
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("error escribiendo archivo temporal de config: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("error reemplazando config atomico: %w", err)
	}

	return nil
}

// SetOwner define el JID del propietario principal y lo persiste.
func (c *Config) SetOwner(ownerJID string, savePath string) error {
	c.mu.Lock()
	c.OwnerJID = ownerJID
	found := false
	for _, u := range c.AllowedUsers {
		if u == ownerJID {
			found = true
			break
		}
	}
	if !found {
		c.AllowedUsers = append(c.AllowedUsers, ownerJID)
	}
	c.mu.Unlock()

	if savePath != "" {
		return c.Save(savePath)
	}
	return nil
}

// SetInferenceCredentials actualiza el endpoint, clave y modelo de inferencia.
func (c *Config) SetInferenceCredentials(apiBase, apiKey, model, savePath string) error {
	c.mu.Lock()
	if apiBase != "" {
		c.APIBase = apiBase
	}
	if apiKey != "" {
		c.APIKey = apiKey
	}
	if model != "" {
		c.Model = model
	}
	c.mu.Unlock()

	if savePath != "" {
		return c.Save(savePath)
	}
	return nil
}
