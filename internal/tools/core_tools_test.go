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
