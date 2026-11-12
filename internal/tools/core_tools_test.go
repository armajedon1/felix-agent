package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateOutput(t *testing.T) {
	short := "salida breve de prueba"
	if res := truncateOutput(short, 100); res != short {
		t.Errorf("se esperaba salida intacta, obtenida: %s", res)
	}

	large := strings.Repeat("A", 15000) + strings.Repeat("B", 15000)
	res := truncateOutput(large, 25000)
	if len(res) >= len(large) {
		t.Errorf("se esperaba reduccion de longitud, obtenida: %d", len(res))
	}
	if !strings.Contains(res, "salida truncada") {
		t.Errorf("se esperaba mensaje de truncamiento en la salida")
	}
	if !strings.HasPrefix(res, "AAAAA") || !strings.HasSuffix(res, "BBBBB") {
		t.Errorf("se esperaba conservar cabecera y cola asimetricas")
	}
}

func TestExecTool_Truncation(t *testing.T) {
	tool := &ExecTool{}
	args, _ := json.Marshal(map[string]any{
		"command": "python3 -c \"print('X' * 35000)\"",
	})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("error ejecutando comando: %v", err)
	}
	if len(out) >= 35000 {
		t.Errorf("se esperaba salida truncada a menos de 35000 caracteres, obtenida: %d", len(out))
	}
	if !strings.Contains(out, "salida truncada") {
		t.Errorf("se esperaba etiqueta de truncamiento")
	}
}

type panickyTool struct{}

func (p *panickyTool) Name() string                { return "panicky" }
func (p *panickyTool) Description() string         { return "herramienta que genera panic" }
func (p *panickyTool) Parameters() map[string]any { return map[string]any{} }
func (p *panickyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	panic("fallo critico simulado")
}

func TestRegistry_PanicRecovery(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&panickyTool{})

	res, err := reg.Execute(context.Background(), "panicky", json.RawMessage("{}"))
	if err == nil {
		t.Fatalf("se esperaba error tras panic, obtenido nil")
	}
	if res != "" {
		t.Errorf("se esperaba resultado vacio, obtenido: %s", res)
	}
	if !strings.Contains(err.Error(), "error interno en herramienta panicky: fallo critico simulado") {
		t.Errorf("mensaje de error inesperado: %v", err)
	}
}

func TestCleanHTML(t *testing.T) {
	htmlInput := `
		<!DOCTYPE html>
		<html>
		<head><style>body { color: red; }</style></head>
		<body>
			<header><p>Cabecera a descartar</p></header>
			<nav><a href="/home">Inicio</a></nav>
			<aside><p>Barra lateral</p></aside>
			<main>
				<h1>Titulo Principal</h1>
				<p>Contenido del articulo con &amp; y &lt;etiquetas&gt;.</p>
				<ul>
					<li>Item 1</li>
					<li>Item 2</li>
				</ul>
			</main>
			<footer><p>Pie de pagina</p></footer>
			<script>console.log("drop me");</script>
		</body>
		</html>
	`
	cleaned := cleanHTML(htmlInput)

	if strings.Contains(cleaned, "Cabecera a descartar") {
		t.Errorf("cleanHTML debio descartar contenido dentro de <header>")
	}
	if strings.Contains(cleaned, "Barra lateral") {
		t.Errorf("cleanHTML debio descartar contenido dentro de <aside>")
	}
	if strings.Contains(cleaned, "Pie de pagina") {
		t.Errorf("cleanHTML debio descartar contenido dentro de <footer>")
	}
	if strings.Contains(cleaned, "Inicio") {
		t.Errorf("cleanHTML debio descartar contenido dentro de <nav>")
	}
	if strings.Contains(cleaned, "drop me") {
		t.Errorf("cleanHTML debio descartar contenido de <script>")
	}
	if !strings.Contains(cleaned, "# Titulo Principal") {
		t.Errorf("se esperaba encabezado convertido a markdown: %s", cleaned)
	}
	if !strings.Contains(cleaned, "Contenido del articulo con & y <etiquetas>.") {
		t.Errorf("se esperaban entidades HTML decodificadas: %s", cleaned)
	}
}
