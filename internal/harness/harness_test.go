package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"felix-go/internal/graph"
)

type mockCompleter struct {
	response string
}

func (m *mockCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return m.response, nil
}

func setupTestHarnessRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s fallo (%v): %s", strings.Join(args, " "), err, string(out))
		}
	}

	run("init")
	run("config", "user.name", "Test Harness")
	run("config", "user.email", "harness@test.local")
	run("config", "commit.gpgsign", "false")

	// Crear archivo main.go basico
	mainFile := filepath.Join(dir, "calc.go")
	code := `package calc

func Multiply(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(mainFile, []byte(code), 0644); err != nil {
		t.Fatalf("error: %v", err)
	}

	run("add", ".")
	run("commit", "-m", "initial commit")

	return dir
}

func TestDevelopmentGraph_FullCycle(t *testing.T) {
	repoDir := setupTestHarnessRepo(t)

	mockResp := `
Modificando funcion Multiply para realizar la multiplicacion real:

### calc.go
<<<<<<< SEARCH
func Multiply(a, b int) int {
	return a + b
}
=======
func Multiply(a, b int) int {
	return a * b
}
>>>>>>> REPLACE
`

	completer := &mockCompleter{response: mockResp}

	g, err := NewDevelopmentGraph(HarnessOptions{
		RepoDir:    repoDir,
		Completer:  completer,
		MaxRetries: 2,
		AutoMerge:  true,
	})
	if err != nil {
		t.Fatalf("error instanciando grafo de desarrollo: %v", err)
	}

	initial := graph.NewState("run-harness-integration")
	initial.SetVar("task_description", "Corregir bug en Multiply para que multiplique")
	// Usar un comando de verificacion rapido para el test
	initial.SetVar("custom_verify_cmd", "echo test_passed")

	finalState, err := g.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("fallo ejecucion del grafo: %v", err)
	}

	if !finalState.GetBool("completed") {
		t.Errorf("se esperaba task completed en estado final")
	}

	// Verificar que el archivo en repoDir fue actualizado y merged a main
	content, err := os.ReadFile(filepath.Join(repoDir, "calc.go"))
	if err != nil {
		t.Fatalf("error leyendo calc.go: %v", err)
	}

	if !strings.Contains(string(content), "return a * b") {
		t.Errorf("codigo modificado no encontrado en repoDir tras merge: %s", string(content))
	}
}
