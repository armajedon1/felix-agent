package checkpoint

import (
	"felix-go/internal/graph"
)

// Store representa la interfaz de persistencia de checkpoints para el motor de grafos.
type Store interface {
	graph.CheckpointStore
	Close() error
}
