package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestGitRepo(t *testing.T) string {
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
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.com")
	run("config", "commit.gpgsign", "false")

	// Crear archivo inicial y primer commit
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("error creando README: %v", err)
	}

	run("add", "README.md")
	run("commit", "-m", "initial commit")

	return dir
}

func TestGitSandbox_Lifecycle(t *testing.T) {
	repoDir := setupTestGitRepo(t)
	sb := NewGitSandbox(repoDir)
	ctx := context.Background()

	runID := "test-sandbox-run"

	// 1. Crear Sandbox
	branch, err := sb.CreateSandbox(ctx, runID)
	if err != nil {
		t.Fatalf("error creando sandbox: %v", err)
	}
	if !strings.HasPrefix(branch, "felix-sandbox/task-") {
		t.Errorf("nombre de rama inesperado: %s", branch)
	}

	// 2. Modificar archivo en el sandbox
	testFile := filepath.Join(repoDir, "feature.txt")
	if err := os.WriteFile(testFile, []byte("feature content\n"), 0644); err != nil {
		t.Fatalf("error escribiendo archivo: %v", err)
	}

	// 3. Commitear
	if err := sb.Commit(ctx, "feat: add feature file"); err != nil {
		t.Fatalf("error en commit: %v", err)
	}

	// 4. Verificar diff
	diff, err := sb.Diff(ctx)
	if err != nil {
		t.Fatalf("error obteniendo diff: %v", err)
	}
	if !strings.Contains(diff, "feature content") {
		t.Errorf("diff no contiene el contenido nuevo: %s", diff)
	}

	// 5. Merge a rama principal
	if err := sb.MergeToMain(ctx); err != nil {
		t.Fatalf("error en merge a main: %v", err)
	}

	// Verificar que el archivo feature.txt sigue existiendo en main
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Errorf("feature.txt no existe en main tras merge")
	}
}

func TestGitSandbox_Discard(t *testing.T) {
	repoDir := setupTestGitRepo(t)
	sb := NewGitSandbox(repoDir)
	ctx := context.Background()

	runID := "test-discard-run"

	_, err := sb.CreateSandbox(ctx, runID)
	if err != nil {
		t.Fatalf("error creando sandbox: %v", err)
	}

	// Modificar archivo
	badFile := filepath.Join(repoDir, "bad.txt")
	if err := os.WriteFile(badFile, []byte("bad code\n"), 0644); err != nil {
		t.Fatalf("error escribiendo archivo: %v", err)
	}
	_ = sb.Commit(ctx, "bad commit")

	// Descartar sandbox
	if err := sb.Discard(ctx); err != nil {
		t.Fatalf("error descartando sandbox: %v", err)
	}

	// Verificar que bad.txt fue eliminado
	if _, err := os.Stat(badFile); !os.IsNotExist(err) {
		t.Errorf("bad.txt debio ser purgado tras discard")
	}
}
