package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"felix-go/internal/tools"
)

// ToolDispatcher define el contrato para la ejecucion aislada de herramientas solicitadas por el modelo.
type ToolDispatcher interface {
	Dispatch(ctx context.Context, sessionID string, isAdmin bool, calls []ToolCall) ([]string, error)
	SetProgressFunc(fn func(sessionID, statusText string))
}

// DefaultToolDispatcher coordina la invocacion de herramientas en serie o paralelo con proteccion de timeouts y memoria.
type DefaultToolDispatcher struct {
	registry   *tools.Registry
	progressFn func(sessionID, statusText string)
}

// NewToolDispatcher inicializa un despachador sobre el registro de herramientas.
func NewToolDispatcher(reg *tools.Registry) *DefaultToolDispatcher {
	return &DefaultToolDispatcher{
		registry: reg,
	}
}

// SetProgressFunc asigna la funcion de callback para notificar progreso a los canales activos.
func (td *DefaultToolDispatcher) SetProgressFunc(fn func(sessionID, statusText string)) {
	td.progressFn = fn
}

// Dispatch ejecuta las herramientas (en serie si es 1 sola, o en paralelo si son multiples) respetando plazos de contexto.
func (td *DefaultToolDispatcher) Dispatch(ctx context.Context, sessionID string, isAdmin bool, calls []ToolCall) ([]string, error) {
	numCalls := len(calls)
	if numCalls == 0 {
		return nil, nil
	}

	outputs := make([]string, numCalls)

	// 1. Notificar progreso a canales interactivos si se detectan herramientas pesadas
	if td.progressFn != nil && !strings.HasPrefix(sessionID, "subagent:") && !strings.HasPrefix(sessionID, "cli-") {
		for _, tc := range calls {
			if hint, ok := heavyTools[tc.Function.Name]; ok {
				td.progressFn(sessionID, fmt.Sprintf("[En progreso]: %s", hint))
				break
			}
		}
	}

	// 2. Ejecucion secuencial si hay un unico llamado
	if numCalls == 1 {
		tc := calls[0]
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 12*time.Second {
			log.Printf("[ToolDispatcher] Cancelando ejecucion de tool %s: tiempo limite casi alcanzado (%v)", tc.Function.Name, time.Until(deadline))
			outputs[0] = "Ejecucion omitida: tiempo limite del turno casi alcanzado. Procede de inmediato a redactar la respuesta final."
			return outputs, nil
		}

		log.Printf("[Tool Call] Invocando %s (ID: %s) Args: %s", tc.Function.Name, tc.ID, tc.Function.Arguments)
		toolCtx := context.WithValue(ctx, "session_id", sessionID)
		toolCtx = context.WithValue(toolCtx, "is_admin", isAdmin)

		out, err := td.registry.Execute(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			out = fmt.Sprintf("Error ejecutando herramienta %s: %v", tc.Function.Name, err)
		}
		if len(out) > 8000 {
			out = out[:8000] + "\n...(salida truncada por longitud)..."
		}
		log.Printf("[Tool Output %s] %d caracteres", tc.Function.Name, len(out))
		outputs[0] = out
		return outputs, nil
	}

	// 3. Ejecucion concurrente paralela si hay multiples llamados
	log.Printf("[ToolDispatcher] Invocando %d tool calls concurrentes en paralelo...", numCalls)
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i, tc := range calls {
		go func(idx int, call ToolCall) {
			defer wg.Done()
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 12*time.Second {
				log.Printf("[ToolDispatcher] Cancelando ejecucion paralela de tool %s: tiempo limite casi alcanzado (%v)", call.Function.Name, time.Until(deadline))
				outputs[idx] = "Ejecucion omitida: tiempo limite del turno casi alcanzado. Procede de inmediato a redactar la respuesta final."
				return
			}

			log.Printf("[Tool Call Paralelo] Invocando %s (ID: %s) Args: %s", call.Function.Name, call.ID, call.Function.Arguments)
			toolCtx := context.WithValue(ctx, "session_id", sessionID)
			toolCtx = context.WithValue(toolCtx, "is_admin", isAdmin)

			out, err := td.registry.Execute(toolCtx, call.Function.Name, json.RawMessage(call.Function.Arguments))
			if err != nil {
				out = fmt.Sprintf("Error ejecutando herramienta %s: %v", call.Function.Name, err)
			}
			if len(out) > 8000 {
				out = out[:8000] + "\n...(salida truncada por longitud)..."
			}
			log.Printf("[Tool Output Paralelo %s] %d caracteres", call.Function.Name, len(out))
			outputs[idx] = out
		}(i, tc)
	}
	wg.Wait()

	return outputs, nil
}
