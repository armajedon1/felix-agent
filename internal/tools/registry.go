package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
)

// Tool define la interfaz estandar que debe implementar cada herramienta en felix-go.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry administra el catalogo de herramientas disponibles para el agente.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	cache *ToolCache
}

// NewRegistry inicializa un registro vacio de herramientas con cache en memoria.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		cache: NewToolCache(),
	}
}

// Register agrega una herramienta al registro.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get obtiene una herramienta por su nombre.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// OpenAIToolSchema representa el esquema de declaracion de herramientas compatible con OpenAI.
type OpenAIToolSchema struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
}

type FunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var adminOnlyTools = map[string]bool{
	"exec":              true,
	"bash":              true,
	"infra":             true,
	"docker":            true,
	"workstation":       true,
	"pc_task":           true,
	"minecraft":         true,
	"sqlite_query":      true,
	"subagent_spawn":    true,
	"skills_write":      true,
	"background_worker": true,
	"file_write":        true,
	"file_edit":         true,
	"file_read":         true,
	"antigravity_cli":   true,
	"gcli":              true,
	"homelab":           true,
	"users_manager":     true,
	"send_message":      true,
	"image_generate":    true,
	"file_ls":           true,
	"apply_patch":       true,
	"process":           true,
}

// GetOpenAISchemas genera la lista de definiciones de herramientas para enviar al LLM.
func (r *Registry) GetOpenAISchemas() []OpenAIToolSchema {
	return r.GetOpenAISchemasForRole(true)
}

// GetOpenAISchemasForRole filtra las herramientas segun el rol del remitente para evitar escalada de privilegios.
func (r *Registry) GetOpenAISchemasForRole(isAdmin bool) []OpenAIToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]OpenAIToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		if !isAdmin && adminOnlyTools[t.Name()] {
			continue // Ocultar herramientas sensibles a contactos no-admin
		}
		schemas = append(schemas, OpenAIToolSchema{
			Type: "function",
			Function: FunctionSchema{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return schemas
}

// Execute ejecuta la herramienta solicitada y retorna su salida (consultando cache si es aplicable).
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("herramienta desconocida: %s", name)
	}

	if r.cache != nil {
		if cached, hit := r.cache.Get(name, args); hit {
			return cached, nil
		}
	}

	var (
		res string
		err error
	)

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[tools] panic recuperado en herramienta %s: %v\nstack:\n%s", name, rec, debug.Stack())
				err = fmt.Errorf("error interno en herramienta %s: %v", name, rec)
			}
		}()
		res, err = t.Execute(ctx, args)
	}()

	if err != nil {
		return "", err
	}

	if r.cache != nil {
		r.cache.Set(name, args, res)
	}
	return res, nil
}
