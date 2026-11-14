package patcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	SearchMarker  = "<<<<<<< SEARCH"
	DividerMarker = "======="
	ReplaceMarker = ">>>>>>> REPLACE"

	DefaultFuzzyThreshold = 0.90
)

// Block representa un bloque atomico de modificacion Search/Replace.
type Block struct {
	FilePath string `json:"file_path"`
	Search   string `json:"search"`
	Replace  string `json:"replace"`
}

var filePathRegex = regexp.MustCompile(`(?i)^\s*(?:#{1,6}\s*)?(?:---|File:|Archivo:|Path:|Ruta:)?\s*[*"']?([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9_]{1,10})[*"']?:?\s*$`)
var fallbackPathRegex = regexp.MustCompile(`(?i)^(?:#{1,6}|---|\*{1,3}|File:|Archivo:|Path:|Ruta:)\s*[*"']?([^\s\n\r"'*]+)`)

func detectFilePath(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if matches := filePathRegex.FindStringSubmatch(line); len(matches) > 1 {
		candidate := strings.Trim(matches[1], "`'\"*:")
		if candidate != "" {
			return candidate
		}
	}
	if matches := fallbackPathRegex.FindStringSubmatch(line); len(matches) > 1 {
		candidate := strings.Trim(matches[1], "`'\"*:")
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ParseBlocks extrae los bloques Search/Replace del texto generado por el LLM.
func ParseBlocks(raw string) ([]Block, error) {
	lines := splitLines(raw)
	var blocks []Block

	i := 0
	lastDetectedPath := ""

	for i < len(lines) {
		line := lines[i]

		// Detectar ruta de archivo si precede al bloque
		if p := detectFilePath(line); p != "" {
			lastDetectedPath = p
		}

		if strings.TrimSpace(line) == SearchMarker {
			searchStart := i + 1
			dividerIdx := -1
			replaceEnd := -1

			// Buscar =======
			for j := searchStart; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == DividerMarker {
					dividerIdx = j
					break
				}
			}

			if dividerIdx == -1 {
				i++
				continue
			}

			// Buscar >>>>>>> REPLACE
			for k := dividerIdx + 1; k < len(lines); k++ {
				if strings.TrimSpace(lines[k]) == ReplaceMarker {
					replaceEnd = k
					break
				}
			}

			if replaceEnd == -1 {
				i = dividerIdx + 1
				continue
			}

			searchContent := strings.Join(lines[searchStart:dividerIdx], "\n")
			replaceContent := strings.Join(lines[dividerIdx+1:replaceEnd], "\n")

			blocks = append(blocks, Block{
				FilePath: lastDetectedPath,
				Search:   searchContent,
				Replace:  replaceContent,
			})

			i = replaceEnd + 1
			continue
		}

		i++
	}

	return blocks, nil
}

// ApplyBlock aplica una sustitucion sobre el contenido de texto (exacta o fuzzy).
func ApplyBlock(content string, b Block) (string, error) {
	// 1. Coincidencia exacta
	if b.Search == "" {
		if content == "" {
			return b.Replace, nil
		}
		return content + "\n" + b.Replace, nil
	}

	if idx := strings.Index(content, b.Search); idx != -1 {
		// Verificar si hay duplicados
		secondIdx := strings.Index(content[idx+len(b.Search):], b.Search)
		if secondIdx == -1 {
			// Coincidencia unica exacta
			return content[:idx] + b.Replace + content[idx+len(b.Search):], nil
		}
		// Si hay duplicados pero exactos, reemplazar la primera aparicion
		return content[:idx] + b.Replace + content[idx+len(b.Search):], nil
	}

	// 2. Coincidencia difusa (Fuzzy Matching tolerante a espaciado)
	startLine, endLine, score, found := FindFuzzyMatch(content, b.Search, DefaultFuzzyThreshold)
	if !found {
		return "", fmt.Errorf("bloque SEARCH no encontrado (similitud maxima alcanzada: %.2f)", score)
	}

	contentLines := splitLines(content)
	replaceLines := splitLines(b.Replace)

	var newLines []string
	newLines = append(newLines, contentLines[:startLine]...)
	newLines = append(newLines, replaceLines...)
	if endLine < len(contentLines) {
		newLines = append(newLines, contentLines[endLine:]...)
	}

	return strings.Join(newLines, "\n"), nil
}

// ApplyFilePatch aplica uno o mas bloques sobre un archivo fisico de forma atomica.
func ApplyFilePatch(filePath string, blocks []Block) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("")
		} else {
			return fmt.Errorf("error leyendo archivo para parche %s: %w", filePath, err)
		}
	}

	content := string(data)

	for idx, b := range blocks {
		updated, err := ApplyBlock(content, b)
		if err != nil {
			return fmt.Errorf("error aplicando bloque #%d en %s: %w", idx+1, filePath, err)
		}
		content = updated
	}

	// Escritura atomica mediante archivo temporal en el mismo directorio
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creando directorio %s: %w", dir, err)
	}
	tmpFile, err := os.CreateTemp(dir, "felix-patch-*.tmp")
	if err != nil {
		return fmt.Errorf("error creando archivo temporal de parche: %w", err)
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("error escribiendo parche temporal: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("error cerrando parche temporal: %w", err)
	}

	// Sustitucion atomica
	if err := os.Rename(tmpName, filePath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("error aplicando parche atomico a %s: %w", filePath, err)
	}

	return nil
}
