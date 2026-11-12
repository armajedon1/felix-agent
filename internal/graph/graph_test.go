package graph

import (
	"context"
	"errors"
	"testing"
)

// memoryStore implementa CheckpointStore en memoria para pruebas rapidas sin SQLite.
type memoryStore struct {
	runs        map[string]string
	checkpoints map[string]map[int]*State
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		runs:        make(map[string]string),
		checkpoints: make(map[string]map[int]*State),
	}
}

func (m *memoryStore) CreateRun(ctx context.Context, runID, graphName string) error {
	m.runs[runID] = "running"
	if _, ok := m.checkpoints[runID]; !ok {
		m.checkpoints[runID] = make(map[int]*State)
	}
	return nil
}

func (m *memoryStore) UpdateRunStatus(ctx context.Context, runID, status string) error {
	m.runs[runID] = status
	return nil
}

func (m *memoryStore) GetRunStatus(ctx context.Context, runID string) (string, error) {
	return m.runs[runID], nil
}

func (m *memoryStore) SaveCheckpoint(ctx context.Context, runID, nodeID string, step int, state *State) error {
	if _, ok := m.checkpoints[runID]; !ok {
		m.checkpoints[runID] = make(map[int]*State)
	}
	m.checkpoints[runID][step] = state.Clone()
	return nil
}

func (m *memoryStore) GetLatestCheckpoint(ctx context.Context, runID string) (*State, error) {
	steps := m.checkpoints[runID]
	var maxStep = -1
	var latest *State
	for s, st := range steps {
		if s > maxStep {
			maxStep = s
			latest = st
		}
	}
	return latest.Clone(), nil
}

func (m *memoryStore) GetCheckpointAtStep(ctx context.Context, runID string, step int) (*State, error) {
	st, ok := m.checkpoints[runID][step]
	if !ok {
		return nil, nil
	}
	return st.Clone(), nil
}

func (m *memoryStore) ListCheckpoints(ctx context.Context, runID string) ([]CheckpointMetadata, error) {
	var list []CheckpointMetadata
	for s, st := range m.checkpoints[runID] {
		list = append(list, CheckpointMetadata{
			RunID:      runID,
			NodeID:     st.CurrentNode,
			StepNumber: s,
		})
	}
	return list, nil
}

func TestLinearGraphExecution(t *testing.T) {
	store := newMemoryStore()

	g := NewStateGraph("linear_pipeline")
	g.SetCheckpointStore(store)

	g.AddNode("nodeA", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("a_done", true)
		return s, nil
	})
	g.AddNode("nodeB", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("b_done", true)
		return s, nil
	})
	g.AddNode("nodeC", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("c_done", true)
		return s, nil
	})

	g.SetEntryPoint("nodeA")
	g.AddEdge("nodeA", "nodeB")
	g.AddEdge("nodeB", "nodeC")
	g.AddEdge("nodeC", END)

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("error compilando grafo lineal: %v", err)
	}

	initial := NewState("test-linear-1")
	finalState, err := compiled.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("error ejecutando grafo: %v", err)
	}

	if !finalState.GetBool("a_done") || !finalState.GetBool("b_done") || !finalState.GetBool("c_done") {
		t.Errorf("no todas las variables fueron asignadas en el flujo lineal")
	}

	if finalState.StepNumber != 3 {
		t.Errorf("se esperaban 3 pasos, obtenidos: %d", finalState.StepNumber)
	}

	status, _ := store.GetRunStatus(context.Background(), "test-linear-1")
	if status != "completed" {
		t.Errorf("se esperaba estado completed, obtenido: %s", status)
	}
}

func TestConditionalRouting(t *testing.T) {
	store := newMemoryStore()

	g := NewStateGraph("conditional_flow")
	g.SetCheckpointStore(store)

	g.AddNode("evaluator", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		val := s.GetInt("score")
		s.SetVar("evaluated", true)
		if val >= 50 {
			s.SetVar("passed", true)
		} else {
			s.SetVar("passed", false)
		}
		return s, nil
	})

	g.AddNode("approved", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("outcome", "aprobado")
		return s, nil
	})

	g.AddNode("rejected", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("outcome", "rechazado")
		return s, nil
	})

	g.SetEntryPoint("evaluator")
	g.AddConditionalEdge("evaluator", func(ctx context.Context, state *State) (string, error) {
		if state.GetBool("passed") {
			return "approved", nil
		}
		return "rejected", nil
	})

	g.AddEdge("approved", END)
	g.AddEdge("rejected", END)

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("error compilando: %v", err)
	}

	// Caso 1: Score alto -> approved
	state1 := NewState("run-cond-high")
	state1.SetVar("score", 85)
	res1, err := compiled.Run(context.Background(), state1)
	if err != nil {
		t.Fatalf("fallo corrida 1: %v", err)
	}
	if res1.GetString("outcome") != "aprobado" {
		t.Errorf("se esperaba aprobado, obtenido: %s", res1.GetString("outcome"))
	}

	// Caso 2: Score bajo -> rejected
	state2 := NewState("run-cond-low")
	state2.SetVar("score", 20)
	res2, err := compiled.Run(context.Background(), state2)
	if err != nil {
		t.Fatalf("fallo corrida 2: %v", err)
	}
	if res2.GetString("outcome") != "rechazado" {
		t.Errorf("se esperaba rechazado, obtenido: %s", res2.GetString("outcome"))
	}
}

func TestSuspensionAndResume(t *testing.T) {
	store := newMemoryStore()

	g := NewStateGraph("hitl_flow")
	g.SetCheckpointStore(store)

	g.AddNode("prepare", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("prep_done", true)
		return s, nil
	})

	g.AddNode("human_approval", func(ctx context.Context, state *State) (*State, error) {
		if !state.GetBool("human_approved") {
			return state, ErrSuspend
		}
		s := state.Clone()
		s.SetVar("approval_processed", true)
		return s, nil
	})

	g.AddNode("deploy", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("deployed", true)
		return s, nil
	})

	g.SetEntryPoint("prepare")
	g.AddEdge("prepare", "human_approval")
	g.AddEdge("human_approval", "deploy")
	g.AddEdge("deploy", END)

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("error compilando: %v", err)
	}

	runID := "run-hitl-test"
	initial := NewState(runID)

	// 1. Ejecutar hasta suspension
	suspendedState, err := compiled.Run(context.Background(), initial)
	if !errors.Is(err, ErrSuspend) {
		t.Fatalf("se esperaba ErrSuspend, obtenido: %v", err)
	}

	status, _ := store.GetRunStatus(context.Background(), runID)
	if status != "suspended" {
		t.Errorf("se esperaba estado 'suspended', obtenido: %s", status)
	}

	// 2. Simular aprobacion humana actualizando el checkpoint mas reciente
	suspendedState.SetVar("human_approved", true)
	_ = store.SaveCheckpoint(context.Background(), runID, "human_approval", suspendedState.StepNumber, suspendedState)

	// 3. Reanudar corrida
	resumedState, err := compiled.Resume(context.Background(), runID)
	if err != nil {
		t.Fatalf("error reanudando corrida: %v", err)
	}

	if !resumedState.GetBool("deployed") {
		t.Errorf("se esperaba despliegue completado tras reanudacion")
	}

	finalStatus, _ := store.GetRunStatus(context.Background(), runID)
	if finalStatus != "completed" {
		t.Errorf("se esperaba estado final 'completed', obtenido: %s", finalStatus)
	}
}

func TestRollbackTimeTravel(t *testing.T) {
	store := newMemoryStore()

	g := NewStateGraph("rollback_flow")
	g.SetCheckpointStore(store)

	g.AddNode("step1", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("version", 1)
		return s, nil
	})
	g.AddNode("step2", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("version", 2)
		return s, nil
	})
	g.AddNode("step3", func(ctx context.Context, state *State) (*State, error) {
		s := state.Clone()
		s.SetVar("version", 3)
		return s, nil
	})

	g.SetEntryPoint("step1")
	g.AddEdge("step1", "step2")
	g.AddEdge("step2", "step3")
	g.AddEdge("step3", END)

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	runID := "run-rollback-test"
	finalState, err := compiled.Run(context.Background(), NewState(runID))
	if err != nil {
		t.Fatalf("error en run: %v", err)
	}
	if finalState.GetInt("version") != 3 {
		t.Fatalf("se esperaba version 3, obtenida: %d", finalState.GetInt("version"))
	}

	// Rollback al paso 1 (version 1)
	rolledBackState, err := compiled.Rollback(context.Background(), runID, 1)
	if err != nil {
		t.Fatalf("error en rollback: %v", err)
	}

	if rolledBackState.GetInt("version") != 1 {
		t.Errorf("se esperaba version 1 tras rollback, obtenida: %d", rolledBackState.GetInt("version"))
	}
	if rolledBackState.CurrentNode != "step1" {
		t.Errorf("se esperaba nodo step1, obtenido: %s", rolledBackState.CurrentNode)
	}
}
