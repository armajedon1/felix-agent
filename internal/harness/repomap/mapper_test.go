package repomap

import (
	"strings"
	"testing"
)

func TestGenerateRepoMap(t *testing.T) {
	// Probar contra el paquete internal/graph del propio proyecto
	mapStr, err := GenerateRepoMap("../../../internal/graph", 10)
	if err != nil {
		t.Fatalf("error generando repomap: %v", err)
	}

	if !strings.Contains(mapStr, "REPOSITORIO AST MAP") {
		t.Errorf("encabezado faltante en repomap")
	}

	if !strings.Contains(mapStr, "type State struct") && !strings.Contains(mapStr, "State") {
		t.Errorf("simbolo State no detectado en repomap: %s", mapStr)
	}

	if !strings.Contains(mapStr, "NewState") && !strings.Contains(mapStr, "Compile") {
		t.Errorf("metodos de StateGraph no detectados: %s", mapStr)
	}
}
