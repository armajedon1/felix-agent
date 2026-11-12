package graph

import (
	"encoding/json"
	"time"
)

// Message representa un mensaje dentro del historial de ejecucion del grafo.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// State representa la memoria y contexto del grafo en un paso dado de ejecucion.
type State struct {
	RunID         string                 `json:"run_id"`
	StepNumber    int                    `json:"step_number"`
	CurrentNode   string                 `json:"current_node"`
	Variables     map[string]interface{} `json:"variables"`
	Messages      []Message              `json:"messages"`
	LastError     string                 `json:"last_error,omitempty"`
	FilesChanged  []string               `json:"files_changed,omitempty"`
	SandboxBranch string                 `json:"sandbox_branch,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
}

// NewState inicializa un nuevo estado para un run dado.
func NewState(runID string) *State {
	return &State{
		RunID:        runID,
		StepNumber:   0,
		Variables:    make(map[string]interface{}),
		Messages:     make([]Message, 0),
		FilesChanged: make([]string, 0),
		CreatedAt:    time.Now().UTC(),
	}
}

// Clone realiza una copia profunda del estado para preservar inmutabilidad entre pasos.
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}

	clone := &State{
		RunID:         s.RunID,
		StepNumber:    s.StepNumber,
		CurrentNode:   s.CurrentNode,
		Variables:     make(map[string]interface{}, len(s.Variables)),
		Messages:      make([]Message, len(s.Messages)),
		LastError:     s.LastError,
		FilesChanged:  make([]string, len(s.FilesChanged)),
		SandboxBranch: s.SandboxBranch,
		CreatedAt:     s.CreatedAt,
	}

	// Copiar variables de forma segura
	for k, v := range s.Variables {
		clone.Variables[k] = v
	}

	copy(clone.Messages, s.Messages)
	copy(clone.FilesChanged, s.FilesChanged)

	return clone
}

// SetVar asigna una variable al estado actual.
func (s *State) SetVar(key string, val interface{}) {
	if s.Variables == nil {
		s.Variables = make(map[string]interface{})
	}
	s.Variables[key] = val
}

// GetVar obtiene una variable del estado.
func (s *State) GetVar(key string) (interface{}, bool) {
	if s.Variables == nil {
		return nil, false
	}
	v, ok := s.Variables[key]
	return v, ok
}

// GetString obtiene una variable casteada a string.
func (s *State) GetString(key string) string {
	if v, ok := s.GetVar(key); ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt obtiene una variable casteada a int.
func (s *State) GetInt(key string) int {
	if v, ok := s.GetVar(key); ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case json.Number:
			if n, err := val.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return 0
}

// GetBool obtiene una variable casteada a bool.
func (s *State) GetBool(key string) bool {
	if v, ok := s.GetVar(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// AppendMessage anade un mensaje al historial.
func (s *State) AppendMessage(role, content string) {
	s.Messages = append(s.Messages, Message{
		Role:    role,
		Content: content,
	})
}

// AddFileChanged registra un archivo modificado si no estaba presente.
func (s *State) AddFileChanged(path string) {
	for _, f := range s.FilesChanged {
		if f == path {
			return
		}
	}
	s.FilesChanged = append(s.FilesChanged, path)
}
