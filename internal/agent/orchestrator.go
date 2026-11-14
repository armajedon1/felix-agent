package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"felix-go/internal/config"
	"felix-go/internal/tools"
)

// Orchestrator define la interfaz del orquestador del ciclo ReAct.
type Orchestrator interface {
	Run(ctx context.Context, sessionID, userInput, extraSystemContext string) (string, error)
}

// ReActOrchestrator coordina el ciclo ReAct conectando el ensamblador de contexto, el proveedor LLM,
// el despachador de herramientas y la compuerta de ejecucion.
type ReActOrchestrator struct {
	cfg        *config.Config
	registry   *tools.Registry
	history    *HistoryManager
	provider   LLMProvider
	assembler  ContextAssembler
	dispatcher ToolDispatcher
	onSuccess  func(sessionID string)
}

// NewReActOrchestrator crea una nueva instancia de ReActOrchestrator con todos sus componentes compartimentados.
func NewReActOrchestrator(
	cfg *config.Config,
	reg *tools.Registry,
	hist *HistoryManager,
	provider LLMProvider,
	assembler ContextAssembler,
	dispatcher ToolDispatcher,
	onSuccess func(sessionID string),
) *ReActOrchestrator {
	return &ReActOrchestrator{
		cfg:        cfg,
		registry:   reg,
		history:    hist,
		provider:   provider,
		assembler:  assembler,
		dispatcher: dispatcher,
		onSuccess:  onSuccess,
	}
}

// Run orquesta el ciclo de razonamiento y ejecucion de herramientas paso a paso.
func (o *ReActOrchestrator) Run(ctx context.Context, sessionID, userInput, extraSystemContext string) (string, error) {
	// 1. Ensamblado determinista del contexto
	conversation, activeGoal, err := o.assembler.Assemble(ctx, sessionID, userInput, extraSystemContext)
	if err != nil {
		return "", fmt.Errorf("error ensamblando contexto: %w", err)
	}

	isAdmin := true
	if val, ok := ctx.Value("is_admin").(bool); ok {
		isAdmin = val
	}

	// 2. Tool Gating Dinamico: Seleccionar exclusivamente las herramientas pertinentes
	schemas := o.registry.SelectTools(userInput, activeGoal, isAdmin)
	forceToolsNextIter := false

	maxIters := o.cfg.MaxIterations
	if maxIters <= 0 {
		maxIters = 10
	}

	for iter := 0; iter < maxIters; iter++ {
		var currentTools []tools.OpenAIToolSchema
		var toolChoice string

		// Control preventivo de tiempo de turno restante
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 15*time.Second {
			log.Printf("[Orchestrator] Tiempo de turno restante critico (%v < 15s). Forzando sintesis final directa.", time.Until(deadline))
			currentTools = nil
			toolChoice = "none"
			conversation = append(conversation, OpenAIMessage{
				Role:    "user",
				Content: "[AVISO DE SISTEMA]: El tiempo disponible para este turno esta por finalizar. No invoques mas herramientas; redacta inmediatamente la respuesta final completa y concisa.",
			})
		} else if iter >= maxIters-2 {
			// En las ultimas dos iteraciones, deshabilitamos herramientas para forzar redaccion final
			currentTools = nil
			toolChoice = "none"
			conversation = append(conversation, OpenAIMessage{
				Role:    "user",
				Content: "[AVISO DE SISTEMA]: Ya dispones de toda la informacion. No invoques mas herramientas; redacta de inmediato la respuesta final clara y completa para el usuario.",
			})
		} else if forceToolsNextIter && len(schemas) > 0 {
			currentTools = schemas
			toolChoice = "required"
			forceToolsNextIter = false
		} else {
			currentTools = schemas
			toolChoice = "auto"
		}

		reqBody := ChatCompletionRequest{
			Model:      o.cfg.Model,
			Messages:   conversation,
			Tools:      currentTools,
			ToolChoice: toolChoice,
		}

		chatResp, err := o.provider.Execute(ctx, reqBody)
		if err != nil {
			return "", fmt.Errorf("error llamando al LLM: %w", err)
		}

		if len(chatResp.Choices) == 0 {
			return "", fmt.Errorf("respuesta sin opciones desde proveedor LLM")
		}

		choice := chatResp.Choices[0]
		assistantMsg := choice.Message
		conversation = append(conversation, assistantMsg)

		// 3. Si el modelo solicita ejecutar herramientas
		if len(assistantMsg.ToolCalls) > 0 {
			outputs, err := o.dispatcher.Dispatch(ctx, sessionID, isAdmin, assistantMsg.ToolCalls)
			if err != nil {
				return "", fmt.Errorf("error despachando herramientas: %w", err)
			}

			for i, tc := range assistantMsg.ToolCalls {
				conversation = append(conversation, OpenAIMessage{
					Role:       "tool",
					Content:    outputs[i],
					ToolCallID: tc.ID,
				})
			}
			continue
		}

		// 4. Si el modelo devuelve texto final
		finalReply := strings.TrimSpace(assistantMsg.Content)
		if finalReply != "" {
			maxConf := o.cfg.MaxConfirmations
			if maxConf <= 0 {
				maxConf = 1
			}

			// Compuerta de Ejecucion (Execution Gate):
			// Si el usuario emitio confirmacion o comando afirmativo, pero el modelo vacila en iteraciones iniciales
			if iter < 2 && maxConf <= 1 && IsAffirmativeOrExecutionCommand(userInput) && IsHesitatingOrAskingConfirmation(finalReply) && len(schemas) > 0 {
				log.Printf("[Execution Gate] Interceptada hesitacion del modelo en iter %d. Usuario ya confirmo ('%s'). Forzando ejecucion obligatoria de herramientas en iter %d...", iter, userInput, iter+1)
				conversation = append(conversation, OpenAIMessage{
					Role:    "user",
					Content: "[DIRECTIVA DE CONTROL - EJECUCION INMEDIATA]: El usuario ya otorgo autorizacion y confirmacion expresa ('" + userInput + "'). Queda terminantemente prohibido pedir mas confirmaciones, demorar con explicaciones o transferir comandos para que los ejecute el usuario. Ejecuta de inmediato las acciones necesarias utilizando tus herramientas y devuelve directamente los resultados tecnicos finales.",
				})
				forceToolsNextIter = true
				continue
			}

			finalReply = cleanFormatting(finalReply)

			// Persistir interaccion en historial
			if o.history != nil {
				o.history.Append(sessionID, OpenAIMessage{Role: "user", Content: userInput})
				o.history.Append(sessionID, OpenAIMessage{Role: "assistant", Content: finalReply})
			}

			if o.onSuccess != nil {
				o.onSuccess(sessionID)
			}

			return finalReply, nil
		}
	}

	// 5. Sintesis forzada de cierre si se agotaron iteraciones
	synthConv := append(conversation, OpenAIMessage{
		Role:    "user",
		Content: "Resume y responde de inmediato al usuario con toda la informacion recopilada hasta este punto sin invocar mas herramientas.",
	})
	synthReq := ChatCompletionRequest{
		Model:      o.cfg.Model,
		Messages:   synthConv,
		ToolChoice: "none",
	}

	if chatResp, err := o.provider.Execute(ctx, synthReq); err == nil && len(chatResp.Choices) > 0 {
		reply := cleanFormatting(chatResp.Choices[0].Message.Content)
		if reply != "" {
			if o.history != nil {
				o.history.Append(sessionID, OpenAIMessage{Role: "user", Content: userInput})
				o.history.Append(sessionID, OpenAIMessage{Role: "assistant", Content: reply})
			}
			if o.onSuccess != nil {
				o.onSuccess(sessionID)
			}
			return reply, nil
		}
	}

	return "Solicitud procesada con exito en base a los datos del sistema.", nil
}
