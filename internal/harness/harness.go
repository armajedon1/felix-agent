package harness

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"felix-go/internal/graph"
	"felix-go/internal/harness/patcher"
	"felix-go/internal/harness/prompts"
	"felix-go/internal/harness/repomap"
	"felix-go/internal/harness/runner"
	"felix-go/internal/harness/sandbox"
)

// LLMCompleter define la interfaz de generacion de texto para el arnes.
type LLMCompleter interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// HarnessOptions configura el subgrafo de ingenieria.
type HarnessOptions struct {
	RepoDir         string
	Completer       LLMCompleter
	CheckpointStore graph.CheckpointStore
	MaxRetries      int
	AutoMerge       bool
}

// NewDevelopmentGraph construye el StateGraph completo para desarrollo nativo.
func NewDevelopmentGraph(opts HarnessOptions) (*graph.CompiledGraph, error) {
	if opts.RepoDir == "" {
		opts.RepoDir = "."
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 4
	}

	sb := sandbox.NewGitSandbox(opts.RepoDir)
	g := graph.NewStateGraph("native_development_harness")
	g.SetMaxSteps(30)
	if opts.CheckpointStore != nil {
		g.SetCheckpointStore(opts.CheckpointStore)
	}

	// 1. Nodo Analizador: extrae mapa sintactico AST
	g.AddNode("node_analyze", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		repoMap, err := repomap.GenerateRepoMap(opts.RepoDir, 35)
		if err != nil {
			return nil, fmt.Errorf("error generando mapa sintactico: %w", err)
		}
		s.SetVar("repo_map", repoMap)
		return s, nil
	})

	// 2. Nodo Planificador: genera el plan de accion y bloques iniciales
	g.AddNode("node_plan", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		taskDesc := s.GetString("task_description")
		if taskDesc == "" {
			return nil, fmt.Errorf("task_description no definida en estado")
		}

		if opts.Completer != nil {
			systemPrompt := prompts.GetSystemEngineerPrompt() + "\n\n" + prompts.GetDiffRules()
			userPrompt := fmt.Sprintf("Tarea solicitada:\n%s\n\nContexto del repositorio:\n%s", taskDesc, s.GetString("repo_map"))

			response, err := opts.Completer.Complete(ctx, systemPrompt, userPrompt)
			if err != nil {
				return nil, fmt.Errorf("error en LLM durante planificacion: %w", err)
			}

			s.SetVar("llm_response", response)
		}

		return s, nil
	})

	// 3. Nodo Sandbox: crea la rama efimera
	g.AddNode("node_sandbox", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		if s.SandboxBranch == "" {
			branch, err := sb.CreateSandbox(ctx, s.RunID)
			if err != nil {
				return nil, fmt.Errorf("error creando rama efimera en sandbox: %w", err)
			}
			s.SandboxBranch = branch
		}
		return s, nil
	})

	// 4. Nodo Patch Engine: aplica los bloques SEARCH/REPLACE
	g.AddNode("node_patch", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		llmOutput := s.GetString("llm_response")
		if llmOutput == "" {
			// Si no hay respuesta previa para parsear, avanzar
			return s, nil
		}

		blocks, err := patcher.ParseBlocks(llmOutput)
		if err != nil {
			return nil, fmt.Errorf("error parseando bloques de parche: %w", err)
		}

		for _, b := range blocks {
			if b.FilePath == "" {
				continue
			}
			fullPath := filepath.Join(opts.RepoDir, b.FilePath)
			if err := patcher.ApplyFilePatch(fullPath, []patcher.Block{b}); err != nil {
				return nil, fmt.Errorf("error aplicando parche en %s: %w", b.FilePath, err)
			}
			s.AddFileChanged(b.FilePath)
		}

		// Realizar commit atomico en sandbox
		_ = sb.Commit(ctx, fmt.Sprintf("harness(step %d): apply patch", s.StepNumber))

		return s, nil
	})

	// 5. Nodo Compilador/Tests: ejecuta suite de verificacion
	g.AddNode("node_verify", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		_, defaultCmd := runner.DetectProject(opts.RepoDir)
		customCmd := s.GetString("custom_verify_cmd")
		verifyCmd := defaultCmd
		if customCmd != "" {
			verifyCmd = customCmd
		}

		if verifyCmd == "" {
			// Si no hay comando de prueba detectado, se considera exitoso
			s.SetVar("verify_success", true)
			return s, nil
		}

		res, err := runner.Execute(ctx, opts.RepoDir, verifyCmd, 60*time.Second)
		if err != nil {
			return nil, fmt.Errorf("error ejecutando verificacion: %w", err)
		}

		if res.Success {
			s.SetVar("verify_success", true)
			s.SetVar("last_error", "")
		} else {
			s.SetVar("verify_success", false)
			s.SetVar("last_error", res.SanitizedError)
			s.SetVar("failed_command", verifyCmd)
			retries := s.GetInt("retries") + 1
			s.SetVar("retries", retries)
		}

		return s, nil
	})

	// 6. Nodo Analizador de Errores: retroalimenta fallos del compilador al LLM
	g.AddNode("node_error_analyzer", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		if opts.Completer != nil {
			cmd := s.GetString("failed_command")
			lastErr := s.GetString("last_error")
			systemPrompt := prompts.GetSystemEngineerPrompt() + "\n\n" + prompts.GetDiffRules()
			userPrompt := prompts.FormatErrorCorrection(cmd, lastErr, fmt.Sprintf("Archivos modificados: %s", strings.Join(s.FilesChanged, ", ")))

			response, err := opts.Completer.Complete(ctx, systemPrompt, userPrompt)
			if err != nil {
				return nil, fmt.Errorf("error en LLM durante correccion de errores: %w", err)
			}

			s.SetVar("llm_response", response)
		}
		return s, nil
	})

	// 7. Nodo Evaluador HITL: verifica si se requiere aprobacion manual
	g.AddNode("node_evaluate_hitl", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		requiresApproval := false
		for _, f := range s.FilesChanged {
			lower := strings.ToLower(f)
			if strings.Contains(lower, "schema") || strings.Contains(lower, "migration") || strings.HasSuffix(lower, ".sql") {
				requiresApproval = true
				break
			}
		}

		if requiresApproval && !s.GetBool("hitl_approved") {
			diff, _ := sb.Diff(ctx)
			s.SetVar("pending_diff", diff)
			return s, graph.ErrSuspend
		}

		return s, nil
	})

	// 8. Nodo Merge y Cierre: fusiona la rama del sandbox a la rama principal
	g.AddNode("node_merge", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		s := state.Clone()
		if opts.AutoMerge {
			if err := sb.MergeToMain(ctx); err != nil {
				return nil, fmt.Errorf("error fusionando sandbox a main: %w", err)
			}
		}
		s.SetVar("completed", true)
		return s, nil
	})

	// 9. Nodo de Fallo por Exceso de Reintentos: suspende para rescate humano
	g.AddNode("node_fail_suspend", func(ctx context.Context, state *graph.State) (*graph.State, error) {
		return state, graph.ErrSuspend
	})

	// Conectividad de nodos
	g.SetEntryPoint("node_analyze")
	g.AddEdge("node_analyze", "node_plan")
	g.AddEdge("node_plan", "node_sandbox")
	g.AddEdge("node_sandbox", "node_patch")
	g.AddEdge("node_patch", "node_verify")

	// Bifurcacion condicional tras verificacion
	g.AddConditionalEdge("node_verify", func(ctx context.Context, state *graph.State) (string, error) {
		if state.GetBool("verify_success") {
			return "node_evaluate_hitl", nil
		}
		if state.GetInt("retries") <= opts.MaxRetries {
			return "node_error_analyzer", nil
		}
		return "node_fail_suspend", nil
	})

	g.AddEdge("node_error_analyzer", "node_patch")
	g.AddEdge("node_evaluate_hitl", "node_merge")
	g.AddEdge("node_merge", graph.END)
	g.AddEdge("node_fail_suspend", graph.END)

	return g.Compile()
}
