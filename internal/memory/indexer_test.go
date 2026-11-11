package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_felix_indexer_*")
	if err != nil {
		t.Fatalf("error creando temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dataDir := filepath.Join(tmpDir, "data")
	workspaceDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(dataDir, 0755)

	// Crear archivos de prueba
	userMD := `# Directivas del Usuario
- El color preferido de Matías es azul naval.
- En el homelab no se usan emojis bajo ninguna circunstancia.
`
	memoryMD := `# Decisiones Tecnicas
## Base de Datos
- Se implemento SQLite en modo WAL con timeout de 10 segundos para persistencia indestructible.
## Hardware
- Servidor Debian en Raspberry Pi 5 con SSD de 256GB.
## Seguridad
- Las claves del homelab estan gestionadas en vaultwarden.
`
	_ = os.WriteFile(filepath.Join(workspaceDir, "USER.md"), []byte(userMD), 0644)
	_ = os.WriteFile(filepath.Join(workspaceDir, "MEMORY.md"), []byte(memoryMD), 0644)

	idx, err := NewIndexer(workspaceDir, dataDir)
	if err != nil {
		t.Fatalf("error creando indexer: %v", err)
	}
	defer idx.Close()

	count, err := idx.Sync()
	if err != nil {
		t.Fatalf("error sincronizando indexer: %v", err)
	}
	if count == 0 {
		t.Fatalf("se esperaban fragmentos indexados, se obtuvieron 0")
	}

	// 1. Probar busqueda por palabra clave exacta
	results, err := idx.Search("azul naval", 5)
	if err != nil {
		t.Fatalf("error buscando 'azul naval': %v", err)
	}
	if len(results) == 0 {
		t.Errorf("se esperaba encontrar resultado para 'azul naval'")
	} else if results[0].FilePath != "USER.md" {
		t.Errorf("se esperaba fuente USER.md, se obtuvo %s", results[0].FilePath)
	}

	// 2. Probar busqueda tecnica
	resultsTech, err := idx.Search("SQLite modo WAL", 5)
	if err != nil {
		t.Fatalf("error buscando 'SQLite modo WAL': %v", err)
	}
	if len(resultsTech) == 0 {
		t.Errorf("se esperaba encontrar resultado para 'SQLite modo WAL'")
	} else if resultsTech[0].FilePath != "MEMORY.md" {
		t.Errorf("se esperaba fuente MEMORY.md, se obtuvo %s", resultsTech[0].FilePath)
	}

	// 3. Probar expansion semantica de sinonimos (contrasenas -> vaultwarden)
	resultsSyn, err := idx.Search("contrasenas", 5)
	if err != nil {
		t.Fatalf("error buscando 'contrasenas': %v", err)
	}
	if len(resultsSyn) == 0 {
		t.Errorf("se esperaba encontrar resultado mediante sinonimo 'contrasenas'")
	} else if resultsSyn[0].FilePath != "MEMORY.md" {
		t.Errorf("se esperaba fuente MEMORY.md, se obtuvo %s", resultsSyn[0].FilePath)
	}
}
