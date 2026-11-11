package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"felix-go/internal/memory"
)

// MemorySearchTool permite al agente consultar semantica y lexicamente toda la memoria historica indexada.
type MemorySearchTool struct {
	Indexer *memory.Indexer
}

func (t *MemorySearchTool) Name() string { return "memory_search" }
func (t *MemorySearchTool) Description() string {
	return "Busca semantica y exhaustivamente en toda la memoria persistente del sistema (USER.md, MEMORY.md, notas diarias historicas de memory/ y documentos del homelab). Retorna los fragmentos mas relevantes con sus fuentes y rutas exactas."
}

func (t *MemorySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Texto o concepto a buscar en la memoria (ej. 'preferencias de cafe', 'decision sobre tailscale', 'fecha del respaldo', 'marián').",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad maxima de fragmentos a recuperar (por defecto 4).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemorySearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Indexer == nil {
		return "El indice semantico de memoria no esta disponible.", nil
	}

	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 4
	}

	results, err := t.Indexer.Search(input.Query, limit)
	if err != nil {
		return fmt.Sprintf("Error ejecutando busqueda semantica: %v", err), nil
	}

	if len(results) == 0 {
		return fmt.Sprintf("No se encontraron registros relevantes en la memoria para la consulta '%s'.", input.Query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Resultados encontrados para '%s' (%d fragmentos recuperados):\n\n", input.Query, len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("--- [Fragmento %d | Fuente: %s | Seccion: %s | Puntuacion: %.2f] ---\n",
			i+1, r.FilePath, r.Title, r.Score))
		sb.WriteString(strings.TrimSpace(r.Content))
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String()), nil
}
