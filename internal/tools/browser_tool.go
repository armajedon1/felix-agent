package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserStep define un paso dentro de una secuencia de automatizacion web.
type BrowserStep struct {
	Action   string `json:"action"`             // navigate, click, input, wait, press_key, screenshot, extract_text, eval
	URL      string `json:"url,omitempty"`      // Para action: navigate
	Selector string `json:"selector,omitempty"` // Para action: click, input, wait, extract_text
	Value    string `json:"value,omitempty"`    // Para action: input (texto) o eval (js script)
	Key      string `json:"key,omitempty"`      // Para action: press_key (Enter, Tab, etc.)
}

// BrowserTool implementa automatizacion de navegador web con Rod y Chromium headless.
type BrowserTool struct {
	DownloadsDir string
}

func (t *BrowserTool) Name() string { return "browser_agent" }
func (t *BrowserTool) Description() string {
	return "Automatiza navegacion e interaccion web con Chromium headless bajo demanda: visitar sitios JS dinamicos, hacer clics en botones/enlaces, completar formularios, tomar capturas de pantalla (screenshots), y extraer texto renderizado. Al finalizar cierra el navegador liberando toda la memoria."
}

func (t *BrowserTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL a visitar (ej: https://example.com).",
			},
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"navigate", "click", "input", "screenshot", "extract_text", "sequence"},
				"description": "Accion directa o 'sequence' para ejecutar varios pasos en orden.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "Selector CSS o texto del elemento a interactuar (para click, input, extract_text).",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Texto a ingresar en el campo seleccionado (para action=input).",
			},
			"screenshot": map[string]any{
				"type":        "boolean",
				"description": "Si es true, toma una captura de pantalla y la guarda en disco para vision o envio.",
			},
			"steps": map[string]any{
				"type":        "array",
				"description": "Lista secuencial de pasos para automatizaciones complejas (login, clicks sucesivos, etc.).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action":   map[string]any{"type": "string"},
						"url":      map[string]any{"type": "string"},
						"selector": map[string]any{"type": "string"},
						"value":    map[string]any{"type": "string"},
						"key":      map[string]any{"type": "string"},
					},
					"required": []string{"action"},
				},
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Tiempo maximo en segundos antes de cancelar (por defecto 35s).",
			},
		},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL            string        `json:"url"`
		Action         string        `json:"action"`
		Selector       string        `json:"selector"`
		Text           string        `json:"text"`
		Screenshot     bool          `json:"screenshot"`
		Steps          []BrowserStep `json:"steps"`
		TimeoutSeconds int           `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	timeout := 40 * time.Second
	if params.TimeoutSeconds > 0 {
		timeout = time.Duration(params.TimeoutSeconds) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Lanzar Chromium headless bajo demanda
	l := launcher.New().
		Bin("/usr/bin/chromium").
		Headless(true).
		NoSandbox(true).
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")

	controlURL, err := l.Context(execCtx).Launch()
	if err != nil {
		return "", fmt.Errorf("error iniciando Chromium headless: %w", err)
	}

	browser := rod.New().ControlURL(controlURL).Context(execCtx)
	if err := browser.Connect(); err != nil {
		return "", fmt.Errorf("error conectando a Chromium: %w", err)
	}
	defer browser.Close()

	downloadsDir := t.DownloadsDir
	if downloadsDir == "" {
		downloadsDir = filepath.Join("data", "downloads")
	}
	_ = os.MkdirAll(downloadsDir, 0755)

	var output strings.Builder
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return "", fmt.Errorf("error creando pagina en Chromium: %w", err)
	}
	defer page.Close()

	// Si se enviaron pasos estructurados en 'steps'
	if len(params.Steps) > 0 || params.Action == "sequence" {
		for i, s := range params.Steps {
			switch s.Action {
			case "navigate":
				if err := page.Navigate(s.URL); err != nil {
					return "", fmt.Errorf("paso %d (navigate %s) fallo: %w", i+1, s.URL, err)
				}
				_ = page.WaitLoad()
				output.WriteString(fmt.Sprintf("Paso %d: Navegado a %s\n", i+1, s.URL))
			case "click":
				el, err := page.Element(s.Selector)
				if err != nil {
					return "", fmt.Errorf("paso %d (click en '%s') fallo: elemento no encontrado (%w)", i+1, s.Selector, err)
				}
				if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
					return "", fmt.Errorf("paso %d (click en '%s') fallo: %w", i+1, s.Selector, err)
				}
				output.WriteString(fmt.Sprintf("Paso %d: Click en selector '%s'\n", i+1, s.Selector))
			case "input":
				el, err := page.Element(s.Selector)
				if err != nil {
					return "", fmt.Errorf("paso %d (input en '%s') fallo: elemento no encontrado (%w)", i+1, s.Selector, err)
				}
				if err := el.SelectAllText(); err == nil {
					_ = el.Input("")
				}
				if err := el.Input(s.Value); err != nil {
					return "", fmt.Errorf("paso %d (input '%s') fallo: %w", i+1, s.Value, err)
				}
				output.WriteString(fmt.Sprintf("Paso %d: Texto ingresado en '%s'\n", i+1, s.Selector))
			case "press_key":
				var k input.Key
				switch strings.ToLower(s.Key) {
				case "enter":
					k = input.Enter
				case "tab":
					k = input.Tab
				case "escape", "esc":
					k = input.Escape
				default:
					k = input.Enter
				}
				_ = page.Keyboard.Press(k)
				output.WriteString(fmt.Sprintf("Paso %d: Tecla presionada '%s'\n", i+1, s.Key))
			case "wait":
				if s.Selector != "" {
					_ = page.WaitElementsMoreThan(s.Selector, 0)
					output.WriteString(fmt.Sprintf("Paso %d: Esperado elemento '%s'\n", i+1, s.Selector))
				} else {
					time.Sleep(1 * time.Second)
				}
			case "screenshot":
				screenPath := filepath.Join(downloadsDir, fmt.Sprintf("browser_step_%d_%d.png", i+1, time.Now().Unix()))
				data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{})
				if err == nil {
					_ = os.WriteFile(screenPath, data, 0644)
					output.WriteString(fmt.Sprintf("Paso %d: Captura guardada en %s (%d bytes)\n", i+1, screenPath, len(data)))
				}
			case "extract_text":
				if s.Selector != "" {
					if el, err := page.Element(s.Selector); err == nil {
						txt, _ := el.Text()
						output.WriteString(fmt.Sprintf("Paso %d [Texto extraido de %s]:\n%s\n", i+1, s.Selector, txt))
					}
				} else {
					txt, _ := page.MustElement("body").Text()
					if len(txt) > 2000 {
						txt = txt[:2000] + "\n...(truncado)"
					}
					output.WriteString(fmt.Sprintf("Paso %d [Texto pagina]:\n%s\n", i+1, txt))
				}
			}
		}
		return output.String(), nil
	}

	// Accion simple individual
	if params.URL == "" {
		return "", errors.New("url es requerida")
	}

	if err := page.Navigate(params.URL); err != nil {
		return "", fmt.Errorf("error navegando a %s: %w", params.URL, err)
	}
	_ = page.WaitLoad()

	title, _ := page.Info()
	pageTitle := ""
	if title != nil {
		pageTitle = title.Title
	}
	output.WriteString(fmt.Sprintf("Pagina cargada: %s (Titulo: '%s')\n", params.URL, pageTitle))

	if params.Action == "click" && params.Selector != "" {
		el, err := page.Element(params.Selector)
		if err != nil {
			return "", fmt.Errorf("elemento '%s' no encontrado para click: %w", params.Selector, err)
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return "", fmt.Errorf("error al hacer click: %w", err)
		}
		output.WriteString(fmt.Sprintf("Click realizado exitosamente en '%s'\n", params.Selector))
		_ = page.WaitIdle(500 * time.Millisecond)
	} else if params.Action == "input" && params.Selector != "" {
		el, err := page.Element(params.Selector)
		if err != nil {
			return "", fmt.Errorf("elemento '%s' no encontrado para input: %w", params.Selector, err)
		}
		if err := el.Input(params.Text); err != nil {
			return "", fmt.Errorf("error escribiendo en '%s': %w", params.Selector, err)
		}
		output.WriteString(fmt.Sprintf("Texto ingresado en '%s'\n", params.Selector))
	}

	if params.Screenshot || params.Action == "screenshot" {
		screenPath := filepath.Join(downloadsDir, fmt.Sprintf("browser_%d.png", time.Now().Unix()))
		data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{})
		if err != nil {
			output.WriteString(fmt.Sprintf("Aviso: Fallo captura de pantalla: %v\n", err))
		} else {
			_ = os.WriteFile(screenPath, data, 0644)
			output.WriteString(fmt.Sprintf("Captura de pantalla guardada en: %s (%d bytes). Puedes verla con vision_inspect o enviarla con send_file.\n", screenPath, len(data)))
		}
	}

	if params.Action == "extract_text" || (params.Action != "click" && params.Action != "input" && !params.Screenshot) {
		bodyEl, err := page.Element("body")
		if err == nil {
			bodyText, _ := bodyEl.Text()
			if len(bodyText) > 3000 {
				bodyText = bodyText[:3000] + "\n...(contenido truncado)"
			}
			output.WriteString(fmt.Sprintf("\n--- Texto extraido de la pagina ---\n%s\n", bodyText))
		}
	}

	return output.String(), nil
}
