package patcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBlocks(t *testing.T) {
	raw := `
Aqui estan los cambios para optimizar el calculo:

### internal/calc/math.go
<<<<<<< SEARCH
func Add(a, b int) int {
	return a + b
}
=======
func Add(a, b int) int {
	// suma segura
	return a + b
}
>>>>>>> REPLACE

Y tambien en python:

File: scripts/worker.py
<<<<<<< SEARCH
def process():
    pass
=======
def process():
    print("ok")
>>>>>>> REPLACE
`

	blocks, err := ParseBlocks(raw)
	if err != nil {
		t.Fatalf("error parseando bloques: %v", err)
	}

	if len(blocks) != 2 {
		t.Fatalf("se esperaban 2 bloques, obtenidos: %d", len(blocks))
	}

	if blocks[0].FilePath != "internal/calc/math.go" {
		t.Errorf("ruta bloque 0 esperada 'internal/calc/math.go', obtenida: %s", blocks[0].FilePath)
	}
	if !strings.Contains(blocks[0].Replace, "suma segura") {
		t.Errorf("bloque 0 replace inesperado: %s", blocks[0].Replace)
	}

	if blocks[1].FilePath != "scripts/worker.py" {
		t.Errorf("ruta bloque 1 esperada 'scripts/worker.py', obtenida: %s", blocks[1].FilePath)
	}
}

func TestApplyBlock_ExactAndFuzzy(t *testing.T) {
	// Caso Go: Exact Match
	goCode := `package main

func ValidateUser(u *User) bool {
	return u.Age >= 18
}

func main() {
	println("iniciado")
}
`

	goBlock := Block{
		Search: `func ValidateUser(u *User) bool {
	return u.Age >= 18
}`,
		Replace: `func ValidateUser(ctx context.Context, u *User) (bool, error) {
	if u == nil {
		return false, errors.New("user nil")
	}
	return u.Age >= 18, nil
}`,
	}

	resGo, err := ApplyBlock(goCode, goBlock)
	if err != nil {
		t.Fatalf("error en match exacto Go: %v", err)
	}
	if !strings.Contains(resGo, "user nil") {
		t.Errorf("reemplazo no aplicado en Go")
	}

	// Caso Python: Fuzzy Match (diferencias de espacios/sangria)
	pyCode := `class TaskProcessor:
    def execute(self, payload):
        data = payload.get("items")
        if not data:
            return None
        return sum(data)
`

	// El LLM genero el search con 2 espacios en vez de 4:
	pyFuzzyBlock := Block{
		Search: `  def execute(self, payload):
    data = payload.get("items")
    if not data:
      return None
    return sum(data)`,
		Replace: `    def execute(self, payload):
        data = payload.get("items")
        if not data:
            return 0
        return sum(data) * 2`,
	}

	resPy, err := ApplyBlock(pyCode, pyFuzzyBlock)
	if err != nil {
		t.Fatalf("error en match difuso Python: %v", err)
	}
	if !strings.Contains(resPy, "return 0") || !strings.Contains(resPy, "sum(data) * 2") {
		t.Errorf("reemplazo difuso no aplicado en Python: %s", resPy)
	}

	// Caso TypeScript: Reemplazo rechazado por no coincidir
	tsCode := `export function greet(name: string): string {
    return "Hello " + name;
}`

	invalidBlock := Block{
		Search: `function calculateTax(subtotal: number): number {
    return subtotal * 0.21;
}`,
		Replace: `function calculateTax() {}`,
	}

	_, err = ApplyBlock(tsCode, invalidBlock)
	if err == nil {
		t.Fatalf("se esperaba error al no encontrar bloque search dispar")
	}
}

func TestApplyFilePatch_Atomic(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "service.go")

	initialContent := `package service

func Start() string {
	return "stopped"
}
`
	if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("error creando archivo: %v", err)
	}

	blocks := []Block{
		{
			Search: `func Start() string {
	return "stopped"
}`,
			Replace: `func Start() string {
	return "running"
}`,
		},
	}

	if err := ApplyFilePatch(filePath, blocks); err != nil {
		t.Fatalf("error aplicando parche atomico: %v", err)
	}

	updated, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("error leyendo archivo modificado: %v", err)
	}

	if !strings.Contains(string(updated), `"running"`) {
		t.Errorf("parche no se reflejo en el archivo: %s", string(updated))
	}
}

func TestApplyFilePatch_NewFile(t *testing.T) {
	tempDir := t.TempDir()
	newFile := filepath.Join(tempDir, "nested", "new_service.go")

	blocks := []Block{
		{
			FilePath: "nested/new_service.go",
			Search:   "",
			Replace:  "package nested\n\nfunc New() string { return \"ok\" }\n",
		},
	}

	if err := ApplyFilePatch(newFile, blocks); err != nil {
		t.Fatalf("error creando nuevo archivo mediante parche: %v", err)
	}

	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("error leyendo nuevo archivo creado: %v", err)
	}

	if !strings.Contains(string(content), "func New()") {
		t.Fatalf("contenido inesperado: %s", string(content))
	}
}
