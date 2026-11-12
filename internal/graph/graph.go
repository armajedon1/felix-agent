package graph

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// END representa el nodo terminal que finaliza la ejecucion del grafo.
const END = "__END__"

var (
	ErrSuspend          = errors.New("grafo suspendido para aprobacion o evento externo")
	ErrMaxStepsExceeded = errors.New("limite de pasos excedido en grafo")
	ErrNoRoute          = errors.New("no se pudo determinar el siguiente nodo")
	ErrNodeNotFound     = errors.New("nodo no registrado en grafo")
)

// NodeFunc es la unidad atomica de ejecucion en el grafo.
type NodeFunc func(ctx context.Context, state *State) (*State, error)

// EdgeRouter evalua el estado y retorna el identificador del siguiente nodo.
type EdgeRouter func(ctx context.Context, state *State) (string, error)

// CheckpointMetadata almacena metadatos basicos de un snapshot de estado.
type CheckpointMetadata struct {
	CheckpointID string    `json:"checkpoint_id"`
	RunID        string    `json:"run_id"`
	NodeID       string    `json:"node_id"`
	StepNumber   int       `json:"step_number"`
	CreatedAt    time.Time `json:"created_at"`
}

// CheckpointStore define el contrato para la persistencia transaccional de estados.
type CheckpointStore interface {
	CreateRun(ctx context.Context, runID, graphName string) error
	UpdateRunStatus(ctx context.Context, runID, status string) error
	GetRunStatus(ctx context.Context, runID string) (string, error)
	SaveCheckpoint(ctx context.Context, runID, nodeID string, step int, state *State) error
	GetLatestCheckpoint(ctx context.Context, runID string) (*State, error)
	GetCheckpointAtStep(ctx context.Context, runID string, step int) (*State, error)
	ListCheckpoints(ctx context.Context, runID string) ([]CheckpointMetadata, error)
}

// StateGraph define la topologia de ejecucion y conectividad de nodos.
type StateGraph struct {
	name             string
	nodes            map[string]NodeFunc
	edges            map[string]string
	conditionalEdges map[string]EdgeRouter
	entryNode        string
	finishNodes      map[string]bool
	checkpointStore  CheckpointStore
	maxSteps         int
}

// NewStateGraph crea una nueva definicion de grafo de estado.
func NewStateGraph(name string) *StateGraph {
	return &StateGraph{
		name:             name,
		nodes:            make(map[string]NodeFunc),
		edges:            make(map[string]string),
		conditionalEdges: make(map[string]EdgeRouter),
		finishNodes:      make(map[string]bool),
		maxSteps:         50,
	}
}

// SetCheckpointStore asocia el almacenamiento transaccional para checkpoints.
func (g *StateGraph) SetCheckpointStore(store CheckpointStore) *StateGraph {
	g.checkpointStore = store
	return g
}

// SetMaxSteps define el limite maximo de pasos por corrida.
func (g *StateGraph) SetMaxSteps(max int) *StateGraph {
	if max > 0 {
		g.maxSteps = max
	}
	return g
}

// AddNode registra una funcion de ejecucion bajo un identificador unico.
func (g *StateGraph) AddNode(id string, fn NodeFunc) *StateGraph {
	g.nodes[id] = fn
	return g
}

// AddEdge conecta de forma directa el nodo 'from' con el nodo 'to'.
func (g *StateGraph) AddEdge(from, to string) *StateGraph {
	g.edges[from] = to
	return g
}

// AddConditionalEdge anade una funcion de enrutamiento dinamico desde el nodo 'from'.
func (g *StateGraph) AddConditionalEdge(from string, router EdgeRouter) *StateGraph {
	g.conditionalEdges[from] = router
	return g
}

// SetEntryPoint define el nodo inicial de ejecucion.
func (g *StateGraph) SetEntryPoint(id string) *StateGraph {
	g.entryNode = id
	return g
}

// AddFinishPoint marca un nodo como terminal.
func (g *StateGraph) AddFinishPoint(id string) *StateGraph {
	g.finishNodes[id] = true
	return g
}

// Compile valida la consistencia del grafo y genera un ejecutable CompiledGraph.
func (g *StateGraph) Compile() (*CompiledGraph, error) {
	if g.entryNode == "" {
		return nil, fmt.Errorf("nodo de entrada no definido")
	}
	if _, ok := g.nodes[g.entryNode]; !ok {
		return nil, fmt.Errorf("nodo de entrada '%s' no existe en catalogo de nodos", g.entryNode)
	}

	// Validar que los destinos de aristas fijas existan o sean END
	for from, to := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("origen de arista '%s' no existe", from)
		}
		if to != END && !g.finishNodes[to] {
			if _, ok := g.nodes[to]; !ok {
				return nil, fmt.Errorf("destino de arista '%s' -> '%s' no existe", from, to)
			}
		}
	}

	return &CompiledGraph{
		name:             g.name,
		nodes:            g.nodes,
		edges:            g.edges,
		conditionalEdges: g.conditionalEdges,
		entryNode:        g.entryNode,
		finishNodes:      g.finishNodes,
		checkpointStore:  g.checkpointStore,
		maxSteps:         g.maxSteps,
	}, nil
}
