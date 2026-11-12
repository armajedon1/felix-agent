package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"felix-go/internal/checkpoint"
	"felix-go/internal/graph"
	"felix-go/internal/harness"
)

// HarnessDevelopTool ejecuta tareas de ingenieria de software utilizando el arnes nativo de grafos de estado.
type HarnessDevelopTool struct {
	DefaultRepoDir  string
	Completer       harness.LLMCompleter
	CheckpointStore checkpoint.Store
}

func (t *HarnessDevelopTool) Name() string { return "harness_develop" }

func (t *HarnessDevelopTool) Description() string {
	return "Ejecuta tareas complejas de desarrollo de software utilizando el arnes nativo en Go basado en grafos de estado: analisis sintactico AST, parches Search/Replace, aislamiento en ramas efimeras de Git y ciclo de verificacion/pruebas."
}

func (t *HarnessDevelopTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Descripcion tecnica detallada del requerimiento, bugfix o refactor a implementar.",
			},
			"repo_dir": map[string]any{
				"type":        "string",
				"description": "Directorio del repositorio de codigo objetivo (por defecto directorio actual o workspace).",
			},
			"verify_cmd": map[string]any{
				"type":        "string",
				"description": "Comando personalizado de verificacion o prueba (opcional; si se omite, se detecta automaticamente).",
			},
			"auto_merge": map[string]any{
				"type":        "boolean",
				"description": "Si es true, fusiona los cambios a la rama principal cuando las pruebas pasan sin errores (default true).",
			},
		},
		"required": []string{"task"},
	}
}

type harnessInput struct {
	Task      string `json:"task"`
	RepoDir   string `json:"repo_dir"`
	VerifyCmd string `json:"verify_cmd"`
	AutoMerge *bool  `json:"auto_merge"`
}

func (t *HarnessDevelopTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in harnessInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("argumentos invalidos: %w", err)
	}

	if strings.TrimSpace(in.Task) == "" {
		return "", fmt.Errorf("el parametro 'task' es obligatorio")
	}

	repoDir := t.DefaultRepoDir
	if in.RepoDir != "" {
		repoDir = in.RepoDir
	}
	if repoDir == "" {
		repoDir = "."
	}

	autoMerge := true
	if in.AutoMerge != nil {
		autoMerge = *in.AutoMerge
	}

	devGraph, err := harness.NewDevelopmentGraph(harness.HarnessOptions{
		RepoDir:         repoDir,
		Completer:       t.Completer,
		CheckpointStore: t.CheckpointStore,
		MaxRetries:      4,
		AutoMerge:       autoMerge,
	})
	if err != nil {
		return "", fmt.Errorf("error construyendo grafo de desarrollo: %w", err)
	}

	runID := fmt.Sprintf("dev-%d", time.Now().UnixNano())
	initialState := graph.NewState(runID)
	initialState.SetVar("task_description", in.Task)
	if in.VerifyCmd != "" {
		initialState.SetVar("custom_verify_cmd", in.VerifyCmd)
	}

	finalState, err := devGraph.Run(ctx, initialState)
	if err != nil && err != graph.ErrSuspend {
		return "", fmt.Errorf("fallo durante ejecucion del arnes de desarrollo: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== REPORTE ARNES DE DESARROLLO (Run: %s) ===\n", runID))
	sb.WriteString(fmt.Sprintf("Directorio: %s\n", repoDir))
	sb.WriteString(fmt.Sprintf("Pasos ejecutados: %d\n", finalState.StepNumber))
	sb.WriteString(fmt.Sprintf("Nodo final: %s\n", finalState.CurrentNode))

	if len(finalState.FilesChanged) > 0 {
		sb.WriteString(fmt.Sprintf("Archivos modificados (%d):\n", len(finalState.FilesChanged)))
		for _, f := range finalState.FilesChanged {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	} else {
		sb.WriteString("Archivos modificados: ninguno\n")
	}

	if finalState.GetBool("completed") {
		sb.WriteString("Estado: COMPLETADO CON EXITO (pruebas superadas y rama fusionada)\n")
	} else if err == graph.ErrSuspend {
		sb.WriteString("Estado: SUSPENDIDO PARA REVISION (HITL o intervencion manual requerida)\n")
		if diff := finalState.GetString("pending_diff"); diff != "" {
			sb.WriteString("\nDiff pendiente:\n" + diff + "\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("Estado: %s\n", finalState.CurrentNode))
		if lastErr := finalState.GetString("last_error"); lastErr != "" {
			sb.WriteString(fmt.Sprintf("Ultimo error del compilador:\n%s\n", lastErr))
		}
	}

	return sb.String(), nil
}
