package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitTool administra operaciones de control de versiones Git de forma nativa y segura.
type GitTool struct {
	DefaultRepoDir string
}

func (t *GitTool) Name() string { return "git_tool" }
func (t *GitTool) Description() string {
	return "Control de versiones Git en repositorios locales del homelab: status, diff, log, branch, commit y push seguro con validacion de secretos."
}

func (t *GitTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "diff", "log", "branch", "commit", "push", "show"},
				"description": "Operacion Git a ejecutar.",
			},
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Ruta del repositorio Git (por defecto el proyecto actual o directorio de trabajo).",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Mensaje de commit (requerido para action='commit'). Prohibicion estricta de emojis.",
			},
			"branch": map[string]any{
				"type":        "string",
				"description": "Nombre de rama para checkout, branch o push.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad de commits a mostrar en log (por defecto 5).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *GitTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action   string `json:"action"`
		RepoPath string `json:"repo_path"`
		Message  string `json:"message"`
		Branch   string `json:"branch"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	repoDir := params.RepoPath
	if repoDir == "" {
		repoDir = t.DefaultRepoDir
	}
	if repoDir == "" {
		repoDir = "."
	}

	// Verificar si existe la carpeta
	if _, err := os.Stat(repoDir); err != nil {
		return "", fmt.Errorf("directorio no encontrado: %s", repoDir)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	runGit := func(gitArgs ...string) (string, error) {
		cmd := exec.CommandContext(timeoutCtx, "git", gitArgs...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		cleanOut := strings.TrimSpace(string(out))
		if err != nil {
			return cleanOut, fmt.Errorf("git %s fallo (%w): %s", gitArgs[0], err, cleanOut)
		}
		return cleanOut, nil
	}

	switch params.Action {
	case "status":
		out, err := runGit("status", "-s")
		if err != nil {
			return "", err
		}
		if out == "" {
			return fmt.Sprintf("Repositorio en `%s`: arbol de trabajo limpio (sin cambios pendientes).", repoDir), nil
		}
		return fmt.Sprintf("*ESTADO GIT EN `%s`*\n```\n%s```", repoDir, out), nil

	case "diff":
		out, err := runGit("diff", "--stat")
		if err != nil {
			return "", err
		}
		detailed, _ := runGit("diff", "-p", "--max-count=100")
		if len(detailed) > 10000 {
			detailed = detailed[:10000] + "\n[... diff truncado por longitud ...]"
		}
		if out == "" {
			return "No hay diferencias sin confirmar en el arbol de trabajo.", nil
		}
		return fmt.Sprintf("*DIFERENCIAS GIT EN `%s`*\n```\n%s```\n\n*Detalle:*\n```diff\n%s```", repoDir, out, detailed), nil

	case "log":
		lim := params.Limit
		if lim <= 0 {
			lim = 5
		}
		out, err := runGit("log", fmt.Sprintf("-n%d", lim), "--oneline", "--decorate")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*HISTORIAL DE COMMITS (`%s`)*\n```\n%s```", repoDir, out), nil

	case "branch":
		if params.Branch != "" {
			out, err := runGit("checkout", params.Branch)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Cambiado a rama `%s`: %s", params.Branch, out), nil
		}
		out, err := runGit("branch", "-a")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*RAMAS DISPONIBLES EN `%s`*\n```\n%s```", repoDir, out), nil

	case "commit":
		if strings.TrimSpace(params.Message) == "" {
			return "", errors.New("parametro 'message' es obligatorio para commit")
		}

		// Validacion de seguridad: evitar commitear credenciales o bases SQLite
		statusOut, _ := runGit("status", "--porcelain")
		lines := strings.Split(statusOut, "\n")
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, ".env") || strings.Contains(lower, ".db") || strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") {
				return "", fmt.Errorf("seguridad: se detecto archivo confidencial en staged ('%s'). Abortando commit preventivamente", strings.TrimSpace(line))
			}
		}

		_, err := runGit("add", "-A")
		if err != nil {
			return "", err
		}
		out, err := runGit("commit", "-m", params.Message)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*Commit realizado exitosamente en `%s`*\n```\n%s```", repoDir, out), nil

	case "push":
		targetBranch := params.Branch
		if targetBranch == "" {
			targetBranch = "main"
		}
		out, err := runGit("push", "origin", targetBranch)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*Push ejecutado exitosamente a origin/%s*\n```\n%s```", targetBranch, out), nil

	case "show":
		out, err := runGit("show", "--stat", "HEAD")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*ULTIMO COMMIT (HEAD)*\n```\n%s```", out), nil

	default:
		return "", fmt.Errorf("accion '%s' no reconocida", params.Action)
	}
}
