package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProjectType identifica el ecosistema del proyecto para inferir el comando de prueba.
type ProjectType string

const (
	ProjectGo     ProjectType = "go"
	ProjectNode   ProjectType = "node"
	ProjectRust   ProjectType = "rust"
	ProjectPython ProjectType = "python"
	ProjectCustom ProjectType = "custom"
)

// RunResult contiene el resultado estructurado de la ejecucion de pruebas/compilacion.
type RunResult struct {
	ProjectType    ProjectType   `json:"project_type"`
	Command        string        `json:"command"`
	ExitCode       int           `json:"exit_code"`
	Success        bool          `json:"success"`
	Stdout         string        `json:"stdout"`
	Stderr         string        `json:"stderr"`
	SanitizedError string        `json:"sanitized_error,omitempty"`
	Duration       time.Duration `json:"duration"`
}

// DetectProject infiere el tipo de proyecto y comando de verificacion predeterminado.
func DetectProject(dir string) (ProjectType, string) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return ProjectGo, "go test -count=1 ./..."
	}
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		return ProjectRust, "cargo test"
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
			return ProjectNode, "pnpm test"
		}
		return ProjectNode, "npm test"
	}
	if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		return ProjectPython, "pytest"
	}
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return ProjectPython, "pytest"
	}

	return ProjectCustom, ""
}

// Execute ejecuta el comando en el directorio con timeout estricto y captura la salida.
func Execute(ctx context.Context, dir, cmdStr string, timeout time.Duration) (*RunResult, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil, fmt.Errorf("comando vacio")
	}

	cmd := exec.CommandContext(execCtx, parts[0], parts[1:]...)
	cmd.Dir = dir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()
	success := exitCode == 0

	pType, _ := DetectProject(dir)

	result := &RunResult{
		ProjectType: pType,
		Command:     cmdStr,
		ExitCode:    exitCode,
		Success:     success,
		Stdout:      stdoutStr,
		Stderr:      stderrStr,
		Duration:    duration,
	}

	if !success {
		result.SanitizedError = SanitizeError(stderrStr, stdoutStr)
	}

	return result, nil
}

var (
	goErrorRegex   = regexp.MustCompile(`(?m)^[^\s]+\.go:\d+:\d+: .*$`)
	testFailRegex  = regexp.MustCompile(`(?m)^--- FAIL: .*$`)
	panicHeadRegex = regexp.MustCompile(`(?m)^panic: .*$`)
)

// SanitizeError extrae las lineas criticas del error para no saturar la ventana de contexto.
func SanitizeError(stderr, stdout string) string {
	combined := stderr + "\n" + stdout
	lines := strings.Split(combined, "\n")

	var criticalLines []string
	captureContext := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if goErrorRegex.MatchString(line) || testFailRegex.MatchString(line) || panicHeadRegex.MatchString(line) {
			criticalLines = append(criticalLines, line)
			captureContext = 3 // Capturar las siguientes 3 lineas para stack o detalle
			continue
		}

		if captureContext > 0 {
			criticalLines = append(criticalLines, line)
			captureContext--
		}
	}

	if len(criticalLines) == 0 {
		// Si no se detectaron patrones tipicos, devolver las ultimas 20 lineas no vacias
		nonEmpty := make([]string, 0, len(lines))
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				nonEmpty = append(nonEmpty, l)
			}
		}
		if len(nonEmpty) > 20 {
			nonEmpty = nonEmpty[len(nonEmpty)-20:]
		}
		return strings.Join(nonEmpty, "\n")
	}

	if len(criticalLines) > 30 {
		criticalLines = criticalLines[:30]
	}

	return strings.Join(criticalLines, "\n")
}
