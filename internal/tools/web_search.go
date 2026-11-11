package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// WebSearchTool busca informacion y enlaces en la web en tiempo real.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Busca informacion, paginas, noticias o documentacion en internet en tiempo real y retorna titulos, URLs y resumenes."
}
func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Terminos de busqueda (ej: 'golang 1.24 release notes', 'debian 12 configure static ip').",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad maxima de resultados (por defecto 5, maximo 10).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Query == "" {
		return "", errors.New("el parametro query no puede estar vacio")
	}
	if params.Limit <= 0 || params.Limit > 10 {
		params.Limit = 5
	}

	// 1. Intentar con Firecrawl CLI
	if out, err := searchWithFirecrawl(ctx, params.Query, params.Limit); err == nil && len(strings.TrimSpace(out)) > 0 {
		return out, nil
	}

	// 2. Fallback a DuckDuckGo HTML
	return searchWithDuckDuckGo(ctx, params.Query, params.Limit)
}

func searchWithFirecrawl(ctx context.Context, query string, limit int) (string, error) {
	cmd := exec.CommandContext(ctx, "firecrawl", "search", query, "--limit", fmt.Sprintf("%d", limit))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	res := strings.TrimSpace(out.String())
	if res == "" {
		return "", errors.New("firecrawl search sin resultados")
	}
	if len(res) > 8000 {
		res = res[:8000] + "\n...(resultados truncados)..."
	}
	return res, nil
}

func searchWithDuckDuckGo(ctx context.Context, query string, limit int) (string, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error conectando con buscador: %w", err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	html := buf.String()

	// Parsear enlaces basicos con regex
	re := regexp.MustCompile(`<a class="result__url" href="([^"]+)">([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(html, limit)

	if len(matches) == 0 {
		return "No se encontraron resultados en la web para la consulta especificada.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Resultados web para '%s':\n\n", query))
	for i, m := range matches {
		u := strings.TrimSpace(m[1])
		title := strings.TrimSpace(m[2])
		sb.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n\n", i+1, title, u))
	}

	return strings.TrimSpace(sb.String()), nil
}
