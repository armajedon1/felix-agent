package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type cacheEntry struct {
	result    string
	expiresAt time.Time
}

// ToolCache almacena en memoria resultados de herramientas de solo lectura con TTL.
type ToolCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttls    map[string]time.Duration
}

// NewToolCache inicializa la cache con las politicas de TTL predeterminadas.
func NewToolCache() *ToolCache {
	return &ToolCache{
		entries: make(map[string]cacheEntry),
		ttls: map[string]time.Duration{
			"weather":     10 * time.Minute,
			"skills_list": 15 * time.Minute,
			"skills_read": 30 * time.Minute,
			"doc_engine":  15 * time.Minute,
			"web_fetch":   5 * time.Minute,
		},
	}
}

func (c *ToolCache) key(toolName string, args []byte) string {
	h := sha256.Sum256(append([]byte(toolName+":"), args...))
	return hex.EncodeToString(h[:])
}

// Get obtiene el resultado cacheado si existe y no ha expirado.
func (c *ToolCache) Get(toolName string, args []byte) (string, bool) {
	if c == nil {
		return "", false
	}
	ttl, cacheable := c.ttls[toolName]
	if !cacheable || ttl <= 0 {
		return "", false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	k := c.key(toolName, args)
	entry, ok := c.entries[k]
	if !ok {
		return "", false
	}

	if time.Now().After(entry.expiresAt) {
		return "", false
	}

	return entry.result, true
}

// Set guarda el resultado de una herramienta en cache segun su TTL asignado.
func (c *ToolCache) Set(toolName string, args []byte, result string) {
	if c == nil {
		return
	}
	ttl, cacheable := c.ttls[toolName]
	if !cacheable || ttl <= 0 || result == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	k := c.key(toolName, args)
	c.entries[k] = cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

// Clear vacia la cache.
func (c *ToolCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cacheEntry)
}
