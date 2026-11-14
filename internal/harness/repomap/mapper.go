package repomap

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SymbolSummary contiene los simbolos exportados de un archivo.
type SymbolSummary struct {
	FilePath string   `json:"file_path"`
	Language string   `json:"language"`
	Symbols  []string `json:"symbols"`
}

var (
	pyClassRegex = regexp.MustCompile(`(?m)^class\s+([A-Za-z0-9_]+(?:\([^)]*\))?):`)
	pyFuncRegex  = regexp.MustCompile(`(?m)^(?:async\s+)?def\s+([A-Za-z0-9_]+\([^)]*\)):`)
	tsExportFunc = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?function\s+([A-Za-z0-9_]+\([^)]*\))`)
	tsExportType = regexp.MustCompile(`(?m)^export\s+(?:type|interface|class)\s+([A-Za-z0-9_]+)`)
)

var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"bin":          true,
	"dist":         true,
	"build":        true,
	"data":         true,
	".cache":       true,
}

// GenerateRepoMap recorre un directorio y construye un mapa sintactico compacto (< 1500 tokens).
func GenerateRepoMap(rootDir string, maxFiles int) (string, error) {
	if maxFiles <= 0 {
		maxFiles = 50
	}

	var summaries []SymbolSummary
	fset := token.NewFileSet()

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if ignoredDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if len(summaries) >= maxFiles {
			return nil
		}

		relPath, _ := filepath.Rel(rootDir, path)
		ext := strings.ToLower(filepath.Ext(path))

		switch ext {
		case ".go":
			if strings.HasSuffix(relPath, "_test.go") {
				return nil
			}
			syms := extractGoSymbols(fset, path)
			if len(syms) > 0 {
				summaries = append(summaries, SymbolSummary{
					FilePath: relPath,
					Language: "go",
					Symbols:  syms,
				})
			}
		case ".py":
			syms := extractPySymbols(path)
			if len(syms) > 0 {
				summaries = append(summaries, SymbolSummary{
					FilePath: relPath,
					Language: "python",
					Symbols:  syms,
				})
			}
		case ".ts", ".js":
			syms := extractTsSymbols(path)
			if len(syms) > 0 {
				summaries = append(summaries, SymbolSummary{
					FilePath: relPath,
					Language: "typescript",
					Symbols:  syms,
				})
			}
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("error recorriendo repositorio: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("=== REPOSITORIO AST MAP ===\n")
	for _, s := range summaries {
		sb.WriteString(fmt.Sprintf("\nArchivo: %s (%s)\n", s.FilePath, s.Language))
		for _, sym := range s.Symbols {
			sb.WriteString("  " + sym + "\n")
		}
	}

	return sb.String(), nil
}

func extractGoSymbols(fset *token.FileSet, path string) []string {
	node, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}

	isMain := node.Name.Name == "main"
	var symbols []string

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !isMain && !d.Name.IsExported() {
				continue
			}
			if isMain && (d.Name.Name == "init" || (d.Name.Name == "main" && d.Recv == nil)) {
				continue
			}
			var buf bytes.Buffer
			var recvStr string
			if d.Recv != nil && len(d.Recv.List) > 0 {
				_ = printer.Fprint(&buf, fset, d.Recv.List[0].Type)
				recvStr = fmt.Sprintf("(%s) ", buf.String())
				buf.Reset()
			}
			_ = printer.Fprint(&buf, fset, d.Type)
			typeStr := strings.TrimPrefix(buf.String(), "func")
			symbols = append(symbols, fmt.Sprintf("func %s%s%s", recvStr, d.Name.Name, typeStr))

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && (isMain || ts.Name.IsExported()) {
						kind := "type"
						switch ts.Type.(type) {
						case *ast.StructType:
							kind = "struct"
						case *ast.InterfaceType:
							kind = "interface"
						}
						symbols = append(symbols, fmt.Sprintf("type %s %s", ts.Name.Name, kind))
					}
				}
			}
		}
	}

	return symbols
}

func extractPySymbols(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	var symbols []string

	for _, m := range pyClassRegex.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			symbols = append(symbols, "class "+m[1])
		}
	}
	for _, m := range pyFuncRegex.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !strings.HasPrefix(m[1], "_") {
			symbols = append(symbols, "def "+m[1])
		}
	}

	return symbols
}

func extractTsSymbols(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	var symbols []string

	for _, m := range tsExportType.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			symbols = append(symbols, "type "+m[1])
		}
	}
	for _, m := range tsExportFunc.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			symbols = append(symbols, "func "+m[1])
		}
	}

	return symbols
}
