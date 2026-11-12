package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// GitSandbox administra ramas efimeras de Git para ejecucion aislada y segura de tareas.
type GitSandbox struct {
	repoDir        string
	originalBranch string
	sandboxBranch  string
	mu             sync.Mutex
}

// NewGitSandbox inicializa el sandbox para el repositorio dado.
func NewGitSandbox(repoDir string) *GitSandbox {
	return &GitSandbox{
		repoDir: repoDir,
	}
}

// runGit ejecuta un comando de git en el directorio del repositorio.
func (s *GitSandbox) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s fallo (%w): %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// CreateSandbox crea una rama efimera felix-sandbox/task-<runID> desde la rama actual.
func (s *GitSandbox) CreateSandbox(ctx context.Context, runID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Obtener rama actual
	current, err := s.runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("error detectando rama actual: %w", err)
	}

	s.originalBranch = current
	s.sandboxBranch = fmt.Sprintf("felix-sandbox/task-%s", runID)

	// Crear y cambiar a la rama efimera
	if _, err := s.runGit(ctx, "checkout", "-b", s.sandboxBranch); err != nil {
		return "", fmt.Errorf("error creando rama efimera %s: %w", s.sandboxBranch, err)
	}

	return s.sandboxBranch, nil
}

// Commit agrega los cambios al stage y realiza un commit atomico en el sandbox.
func (s *GitSandbox) Commit(ctx context.Context, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.runGit(ctx, "add", "-A"); err != nil {
		return fmt.Errorf("error en git add: %w", err)
	}

	// Comprobar si hay cambios staged antes de commitear
	status, err := s.runGit(ctx, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("error verificando git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return nil // No hay cambios para commitear
	}

	if _, err := s.runGit(ctx, "commit", "-m", message); err != nil {
		return fmt.Errorf("error en git commit: %w", err)
	}

	return nil
}

// Diff retorna el diff unificado entre la rama original y la rama sandbox.
func (s *GitSandbox) Diff(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.originalBranch == "" {
		return s.runGit(ctx, "diff")
	}

	return s.runGit(ctx, "diff", s.originalBranch+"...HEAD")
}

// MergeToMain fusiona la rama efimera en la rama original y elimina la efimera.
func (s *GitSandbox) MergeToMain(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.originalBranch == "" || s.sandboxBranch == "" {
		return fmt.Errorf("sandbox no inicializado activamente")
	}

	// Volver a la rama original
	if _, err := s.runGit(ctx, "checkout", s.originalBranch); err != nil {
		return fmt.Errorf("error volviendo a rama %s: %w", s.originalBranch, err)
	}

	// Intentar fast-forward merge
	if _, err := s.runGit(ctx, "merge", "--ff-only", s.sandboxBranch); err != nil {
		// Fallback a merge estandar si no es fast-forward puro
		if _, mergeErr := s.runGit(ctx, "merge", "--no-edit", s.sandboxBranch); mergeErr != nil {
			return fmt.Errorf("error fusionando rama %s en %s: %w", s.sandboxBranch, s.originalBranch, mergeErr)
		}
	}

	// Eliminar rama efimera
	_, _ = s.runGit(ctx, "branch", "-D", s.sandboxBranch)
	s.sandboxBranch = ""

	return nil
}

// Discard descarta todos los cambios del sandbox, vuelve a la rama original y elimina la efimera.
func (s *GitSandbox) Discard(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.originalBranch == "" {
		return nil
	}

	// Limpiar cambios no commiteados
	_, _ = s.runGit(ctx, "reset", "--hard")
	_, _ = s.runGit(ctx, "clean", "-fd")

	// Volver a la rama original
	_, _ = s.runGit(ctx, "checkout", s.originalBranch)

	// Eliminar la rama efimera si existia
	if s.sandboxBranch != "" {
		_, _ = s.runGit(ctx, "branch", "-D", s.sandboxBranch)
		s.sandboxBranch = ""
	}

	return nil
}

// GetSandboxBranch retorna el nombre de la rama sandbox activa.
func (s *GitSandbox) GetSandboxBranch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sandboxBranch
}

// GetOriginalBranch retorna la rama base desde donde se creo el sandbox.
func (s *GitSandbox) GetOriginalBranch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.originalBranch
}
