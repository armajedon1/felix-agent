package mcp

import (
	"context"
	"log"
	"sync"
	"time"

	"felix-go/internal/tools"
)

// Manager supervisa el ciclo de vida de multiples clientes MCP.
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client
}

// NewManager crea un nuevo administrador MCP.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*Client),
	}
}

// RegisterServers inicia los servidores configurados, descubre sus herramientas y las registra en el catalogo.
func (m *Manager) RegisterServers(ctx context.Context, servers map[string]ServerConfig, reg *tools.Registry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cfg := range servers {
		startCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		client, err := StartClient(startCtx, name, cfg)
		cancel()
		if err != nil {
			log.Printf("[MCP Manager Warning] No se pudo iniciar servidor '%s': %v", name, err)
			continue
		}

		m.clients[name] = client

		listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
		toolDefs, err := client.ListTools(listCtx)
		listCancel()
		if err != nil {
			log.Printf("[MCP Manager Warning] Error listando herramientas de '%s': %v", name, err)
			continue
		}

		for _, def := range toolDefs {
			adapted := NewMCPAdaptedTool(client, name, def)
			reg.Register(adapted)
			log.Printf("[MCP Manager] Registrada herramienta '%s' desde servidor '%s'", adapted.Name(), name)
		}
	}

	return nil
}

// Close cierra todos los subprocesos de servidores MCP activos.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			log.Printf("[MCP Manager] Error cerrando '%s': %v", name, err)
		}
	}
}
