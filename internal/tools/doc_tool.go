package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DocEngineTool procesa, extrae texto y convierte documentos ofimaticos (ODF: .odt, .ods, .odp, .docx, .pdf).
type DocEngineTool struct{}

func (t *DocEngineTool) Name() string { return "doc_engine" }
func (t *DocEngineTool) Description() string {
	return "Lee, extrae texto limpio y estructura de documentos ofimaticos abiertos ODF (.odt, .ods, .odp), Word (.docx) y archivos PDF de forma rapida y nativa."
}
func (t *DocEngineTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"read", "info"},
				"description": "Accion a realizar: 'read' para extraer el texto/contenido o 'info' para metadatos basicos.",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Ruta absoluta o relativa al archivo (.odt, .ods, .odp, .docx, .pdf, .txt).",
			},
		},
		"required": []string{"action", "file_path"},
	}
}

func (t *DocEngineTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action   string `json:"action"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.FilePath == "" {
		return "", errors.New("file_path no puede estar vacio")
	}

	info, err := os.Stat(params.FilePath)
	if err != nil {
		return "", fmt.Errorf("error accediendo a archivo: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(params.FilePath))

	if params.Action == "info" {
		return fmt.Sprintf("Archivo: %s\nFormato: %s\nTamano: %d bytes\nModificado: %s",
			filepath.Base(params.FilePath), ext, info.Size(), info.ModTime().Format("2006-01-02 15:04:05")), nil
	}

	switch ext {
	case ".odt", ".ods", ".odp":
		return extractODF(params.FilePath, ext)
	case ".docx":
		return extractDocx(params.FilePath)
	case ".pdf":
		return extractPDF(ctx, params.FilePath)
	case ".txt", ".md", ".csv", ".json", ".log":
		data, err := os.ReadFile(params.FilePath)
		if err != nil {
			return "", err
		}
		res := string(data)
		if len(res) > 10000 {
			res = res[:10000] + "\n...(truncado por longitud)..."
		}
		return res, nil
	default:
		return "", fmt.Errorf("formato no soportado directamente: %s (soportados: .odt, .ods, .odp, .docx, .pdf, .txt, .md)", ext)
	}
}

// extractODF extrae el contenido de texto y tablas de archivos OpenDocument (.odt, .ods, .odp).
func extractODF(filePath, ext string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("error abriendo archivo ODF como archivo zip: %w", err)
	}
	defer r.Close()

	var contentFile *zip.File
	for _, f := range r.File {
		if f.Name == "content.xml" {
			contentFile = f
			break
		}
	}
	if contentFile == nil {
		return "", errors.New("el archivo ODF no contiene content.xml")
	}

	rc, err := contentFile.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	decoder := xml.NewDecoder(rc)
	var sb strings.Builder
	var inParagraph bool
	var inCell bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("error parseando XML ODF: %w", err)
		}

		switch elem := token.(type) {
		case xml.StartElement:
			name := elem.Name.Local
			if name == "p" || name == "h" {
				inParagraph = true
			} else if name == "table-row" {
				sb.WriteString("\n| ")
			} else if name == "table-cell" {
				inCell = true
			}
		case xml.EndElement:
			name := elem.Name.Local
			if name == "p" || name == "h" {
				if inParagraph {
					sb.WriteString("\n")
					inParagraph = false
				}
			} else if name == "table-cell" {
				if inCell {
					sb.WriteString(" | ")
					inCell = false
				}
			}
		case xml.CharData:
			text := string(elem)
			if strings.TrimSpace(text) != "" {
				sb.WriteString(text)
			}
		}

		if sb.Len() > 15000 {
			sb.WriteString("\n...(contenido ODF truncado por longitud)...")
			break
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "(Documento ODF vacio o sin texto legible)", nil
	}
	return out, nil
}

// extractDocx extrae parrafos de texto de un archivo Word (.docx).
func extractDocx(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("error abriendo archivo docx: %w", err)
	}
	defer r.Close()

	var docFile *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", errors.New("el archivo docx no contiene word/document.xml")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	decoder := xml.NewDecoder(rc)
	var sb strings.Builder

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("error parseando XML docx: %w", err)
		}

		switch elem := token.(type) {
		case xml.StartElement:
			if elem.Name.Local == "p" {
				sb.WriteString("\n")
			}
		case xml.CharData:
			sb.WriteString(string(elem))
		}

		if sb.Len() > 15000 {
			sb.WriteString("\n...(contenido docx truncado por longitud)...")
			break
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "(Documento docx vacio o sin texto legible)", nil
	}
	return out, nil
}

// extractPDF extrae texto de un archivo PDF usando la libreria de Python pypdf instalada en el sistema.
func extractPDF(ctx context.Context, filePath string) (string, error) {
	pyCode := `
import pypdf, sys
reader = pypdf.PdfReader(sys.argv[1])
pages = []
for i, page in enumerate(reader.pages):
    txt = page.extract_text()
    if txt:
        pages.append(f"--- Pagina {i+1} ---\n{txt}")
print("\n\n".join(pages))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", pyCode, filePath)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error extrayendo PDF (%s): %v", strings.TrimSpace(errBuf.String()), err)
	}

	text := strings.TrimSpace(outBuf.String())
	if len(text) > 15000 {
		text = text[:15000] + "\n...(contenido PDF truncado por longitud)..."
	}
	if text == "" {
		return "(Archivo PDF sin capas de texto extraibles)", nil
	}
	return text, nil
}
