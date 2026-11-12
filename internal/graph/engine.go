package graph

import (
	"context"
	"errors"
	"fmt"
)

// CompiledGraph representa la estructura ejecutable y optimizada del grafo de estado.
type CompiledGraph struct {
	name             string
	nodes            map[string]NodeFunc
	edges            map[string]string
	conditionalEdges map[string]EdgeRouter
	entryNode        string
	finishNodes      map[string]bool
	checkpointStore  CheckpointStore
	maxSteps         int
}

// Run inicia la ejecucion del grafo desde su nodo de entrada.
func (cg *CompiledGraph) Run(ctx context.Context, initialState *State) (*State, error) {
	if initialState == nil {
		return nil, fmt.Errorf("estado inicial nulo")
	}

	state := initialState.Clone()
	if state.RunID == "" {
		return nil, fmt.Errorf("run_id obligatorio en estado inicial")
	}

	if cg.checkpointStore != nil {
		if err := cg.checkpointStore.CreateRun(ctx, state.RunID, cg.name); err != nil {
			return nil, fmt.Errorf("error inicializando run en checkpoint store: %w", err)
		}
	}

	return cg.executeLoop(ctx, state, cg.entryNode)
}

// Resume retoma la ejecucion de un run previamente suspendido o interrumpido.
func (cg *CompiledGraph) Resume(ctx context.Context, runID string) (*State, error) {
	if cg.checkpointStore == nil {
		return nil, fmt.Errorf("checkpoint store no configurado para reanudacion")
	}

	lastState, err := cg.checkpointStore.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("error cargando ultimo checkpoint: %w", err)
	}
	if lastState == nil {
		return nil, fmt.Errorf("no se encontro checkpoint para run_id: %s", runID)
	}

	// Determinar el siguiente nodo a partir del ultimo nodo ejecutado
	nextNode, err := cg.resolveNextNode(ctx, lastState.CurrentNode, lastState)
	if err != nil {
		return nil, fmt.Errorf("error determinando nodo para reanudacion: %w", err)
	}
	if nextNode == END || nextNode == "" {
		_ = cg.checkpointStore.UpdateRunStatus(ctx, runID, "completed")
		return lastState, nil
	}

	if err := cg.checkpointStore.UpdateRunStatus(ctx, runID, "running"); err != nil {
		return nil, fmt.Errorf("error actualizando estado de corrida a running: %w", err)
	}

	return cg.executeLoop(ctx, lastState, nextNode)
}

// Rollback retrocede el estado al paso indicado y lo marca como suspendido para analisis o reintento.
func (cg *CompiledGraph) Rollback(ctx context.Context, runID string, stepNumber int) (*State, error) {
	if cg.checkpointStore == nil {
		return nil, fmt.Errorf("checkpoint store no configurado para rollback")
	}

	state, err := cg.checkpointStore.GetCheckpointAtStep(ctx, runID, stepNumber)
	if err != nil {
		return nil, fmt.Errorf("error recuperando checkpoint en paso %d: %w", stepNumber, err)
	}
	if state == nil {
		return nil, fmt.Errorf("checkpoint no encontrado para run %s en paso %d", runID, stepNumber)
	}

	if err := cg.checkpointStore.UpdateRunStatus(ctx, runID, "suspended"); err != nil {
		return nil, fmt.Errorf("error actualizando estado a suspended tras rollback: %w", err)
	}

	return state, nil
}

// executeLoop gestiona el ciclo determinista de ejecucion paso a paso.
func (cg *CompiledGraph) executeLoop(ctx context.Context, state *State, startNode string) (*State, error) {
	currentNode := startNode
	currentState := state.Clone()

	for step := currentState.StepNumber + 1; step <= cg.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, currentState.RunID, "suspended")
			}
			return currentState, err
		}

		fn, ok := cg.nodes[currentNode]
		if !ok {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, currentState.RunID, "failed")
			}
			return currentState, fmt.Errorf("%w: %s", ErrNodeNotFound, currentNode)
		}

		currentState.StepNumber = step
		currentState.CurrentNode = currentNode

		nextState, err := fn(ctx, currentState)
		if errors.Is(err, ErrSuspend) {
			if nextState == nil {
				nextState = currentState
			}
			nextState.CurrentNode = currentNode
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.SaveCheckpoint(ctx, nextState.RunID, currentNode, step, nextState)
				_ = cg.checkpointStore.UpdateRunStatus(ctx, nextState.RunID, "suspended")
			}
			return nextState, ErrSuspend
		}
		if err != nil {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, currentState.RunID, "failed")
			}
			return currentState, err
		}

		if nextState == nil {
			nextState = currentState
		}
		nextState.StepNumber = step
		nextState.CurrentNode = currentNode

		if cg.checkpointStore != nil {
			if err := cg.checkpointStore.SaveCheckpoint(ctx, nextState.RunID, currentNode, step, nextState); err != nil {
				return nil, fmt.Errorf("error guardando checkpoint: %w", err)
			}
		}

		// Comprobar si el nodo actual esta marcado explicitamente como de finalizacion
		if cg.finishNodes[currentNode] {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, nextState.RunID, "completed")
			}
			return nextState, nil
		}

		// Enrutar al siguiente nodo
		nextNode, err := cg.resolveNextNode(ctx, currentNode, nextState)
		if err != nil {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, nextState.RunID, "failed")
			}
			return nextState, err
		}

		if nextNode == END || nextNode == "" {
			if cg.checkpointStore != nil {
				_ = cg.checkpointStore.UpdateRunStatus(ctx, nextState.RunID, "completed")
			}
			return nextState, nil
		}

		currentNode = nextNode
		currentState = nextState
	}

	if cg.checkpointStore != nil {
		_ = cg.checkpointStore.UpdateRunStatus(ctx, currentState.RunID, "failed")
	}
	return currentState, ErrMaxStepsExceeded
}

// resolveNextNode determina la transicion evaluando primero aristas condicionales y luego fijas.
func (cg *CompiledGraph) resolveNextNode(ctx context.Context, currentNode string, state *State) (string, error) {
	if router, ok := cg.conditionalEdges[currentNode]; ok {
		next, err := router(ctx, state)
		if err != nil {
			return "", fmt.Errorf("error en arista condicional desde '%s': %w", currentNode, err)
		}
		return next, nil
	}

	if next, ok := cg.edges[currentNode]; ok {
		return next, nil
	}

	// Si no tiene aristas de salida, finaliza
	return END, nil
}
