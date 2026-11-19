package botfactory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type LLMCompleter interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type Manager struct {
	storage   *Storage
	completer LLMCompleter
	mu        sync.RWMutex
}

func NewManager(storage *Storage, completer LLMCompleter) *Manager {
	return &Manager{
		storage:   storage,
		completer: completer,
	}
}

func (m *Manager) Storage() *Storage {
	return m.storage
}

func (m *Manager) SetCompleter(completer LLMCompleter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completer = completer
}

func (m *Manager) CreateBot(spec *BotSpec) error {
	if spec.ID == "" {
		return fmt.Errorf("el ID del bot no puede estar vacio")
	}
	if spec.Name == "" {
		spec.Name = spec.ID
	}
	if spec.Type == "" {
		spec.Type = TypeCustom
	}
	return m.storage.SaveBot(spec)
}

func (m *Manager) CreateFromTemplate(templateName, customID, customName string, customCatalog []CatalogItem) (*BotSpec, error) {
	var spec *BotSpec
	switch strings.ToLower(templateName) {
	case "pizzeria", "pizza", "restaurante":
		spec = TemplatePizzeria()
	case "turnos", "citas", "peluqueria", "estetica":
		spec = TemplateAppointments()
	case "llamadas", "calls", "demo":
		spec = TemplateCallScheduler()
	case "faq", "faq_soporte", "soporte", "atencion":
		spec = TemplateFAQSupport()
	default:
		return nil, fmt.Errorf("plantilla '%s' no reconocida. Opciones disponibles: pizzeria, turnos, llamadas, faq_soporte", templateName)
	}

	if customID != "" {
		spec.ID = customID
	}
	if customName != "" {
		spec.Name = customName
	}
	if len(customCatalog) > 0 {
		spec.Catalog = customCatalog
	}

	spec.CreatedAt = time.Now().UTC()
	spec.UpdatedAt = time.Now().UTC()

	if err := m.storage.SaveBot(spec); err != nil {
		return nil, fmt.Errorf("error guardando bot desde plantilla: %w", err)
	}

	return spec, nil
}

func (m *Manager) GetBot(id string) (*BotSpec, error) {
	return m.storage.GetBot(id)
}

func (m *Manager) ListBots() ([]*BotSpec, error) {
	return m.storage.ListBots()
}

func (m *Manager) DeleteBot(id string) error {
	return m.storage.DeleteBot(id)
}

func (m *Manager) Chat(ctx context.Context, botID, sessionID, userMsg string) (*ChatResult, error) {
	spec, err := m.storage.GetBot(botID)
	if err != nil {
		return nil, err
	}

	if spec.Status == "paused" {
		return &ChatResult{
			Reply: "El servicio de atención se encuentra momentáneamente pausado. Por favor intenta más tarde.",
		}, nil
	}

	m.mu.RLock()
	completer := m.completer
	m.mu.RUnlock()

	if completer == nil {
		return nil, fmt.Errorf("no hay motor de inferencia LLM configurado para procesar chats")
	}

	recentMsgs, err := m.storage.GetRecentMessages(botID, sessionID, 8)
	if err != nil {
		recentMsgs = nil
	}

	systemPrompt := m.buildSystemPrompt(spec, sessionID)
	userPrompt := m.buildUserPrompt(recentMsgs, userMsg)

	rawResponse, err := completer.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("error obteniendo respuesta del LLM para el bot '%s': %w", botID, err)
	}

	result := &ChatResult{}
	cleanReply, actionJSON := extractActionBlock(rawResponse)
	result.Reply = strings.TrimSpace(cleanReply)

	if actionJSON != "" {
		m.processAction(spec, sessionID, actionJSON, result)
	}

	_ = m.storage.SaveMessage(&ChatMessage{
		BotID:     botID,
		SessionID: sessionID,
		Role:      "user",
		Content:   userMsg,
	})

	_ = m.storage.SaveMessage(&ChatMessage{
		BotID:     botID,
		SessionID: sessionID,
		Role:      "assistant",
		Content:   result.Reply,
	})

	return result, nil
}

func (m *Manager) buildSystemPrompt(spec *BotSpec, sessionID string) string {
	var sb strings.Builder

	sb.WriteString(spec.SystemPrompt)
	sb.WriteString("\n\n")

	sb.WriteString("DIRECTIVAS CRITICAS DE OPERACION Y ANTI-ALUCINACION:\n")
	sb.WriteString("1. Responde UNICAMENTE basandote en la informacion, productos, servicios y preguntas frecuentes provistas a continuacion.\n")
	sb.WriteString("2. PROHIBIDO inventar precios, articulos, disponibilidad o politicas no especificadas.\n")
	sb.WriteString("3. Si el usuario consulta por algo fuera de este contexto, declina amablemente indicando que no dispones de esa informacion.\n")
	sb.WriteString("4. Manten un tono profesional, claro, conciso y cordial.\n\n")

	if len(spec.Catalog) > 0 {
		sb.WriteString("CATALOGO OFICIAL DE PRODUCTOS / SERVICIOS DISPONIBLES:\n")
		for _, item := range spec.Catalog {
			if !item.Available {
				continue
			}
			desc := ""
			if item.Description != "" {
				desc = fmt.Sprintf(" - %s", item.Description)
			}
			sb.WriteString(fmt.Sprintf("- ID: %s | Nombre: %s | Categoria: %s | Precio: $%.2f%s\n", item.ID, item.Name, item.Category, item.Price, desc))
		}
		sb.WriteString("\n")
	}

	if len(spec.FAQ) > 0 {
		sb.WriteString("PREGUNTAS FRECUENTES Y POLITICAS DEL NEGOCIO:\n")
		for _, f := range spec.FAQ {
			sb.WriteString(fmt.Sprintf("P: %s\nR: %s\n\n", f.Question, f.Answer))
		}
	}

	if spec.BusinessHours != nil {
		sb.WriteString(fmt.Sprintf("HORARIOS DE ATENCION: Dias: %s | Horario: %s a %s\n\n",
			strings.Join(spec.BusinessHours.Days, ", "), spec.BusinessHours.OpenTime, spec.BusinessHours.CloseTime))
	}

	if spec.Type == TypeAppointmentBooking {
		apps, err := m.storage.ListAppointments(spec.ID, 15)
		if err == nil && len(apps) > 0 {
			sb.WriteString("TURNOS YA OCUPADOS EN AGENDA (NO DISPONIBLES PARA AGENDAR):\n")
			for _, a := range apps {
				sb.WriteString(fmt.Sprintf("- %s (Servicio: %s)\n", a.StartTime.Format("2006-01-02 15:04"), a.Service))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("FORMATO DE ACCIONES Y REGISTRO:\n")
	if spec.Type == TypeOrderTaking {
		sb.WriteString(`Cuando el cliente confirme TODOS los datos requeridos (productos y cantidades, nombre, direccion de entrega y metodo de pago), incluye al final de tu mensaje un bloque de accion exacto:
` + "```json:action\n" +
			`{
  "action": "create_order",
  "customer_name": "Nombre Cliente",
  "delivery_address": "Calle y Altura",
  "payment_method": "efectivo | transferencia",
  "items": [{"id": "ID_DEL_CATALOGO", "qty": 1}]
}` + "\n```\n" +
			"Si aun faltan datos, NO incluyas el bloque json:action y pregunta cordialmente por el dato faltante.\n")
	} else if spec.Type == TypeAppointmentBooking {
		sb.WriteString(`Cuando el cliente confirme TODOS los datos requeridos (nombre, telefono, servicio, fecha y hora de atencion), incluye al final de tu mensaje un bloque de accion exacto:
` + "```json:action\n" +
			`{
  "action": "create_appointment",
  "customer_name": "Nombre Cliente",
  "customer_phone": "` + sessionID + `",
  "service": "Nombre del Servicio",
  "date_time": "2026-11-20T15:00:00"
}` + "\n```\n" +
			"Si aun faltan datos o el horario esta ocupado, NO incluyas el bloque json:action y solicita un horario disponible.\n")
	}

	return sb.String()
}

func (m *Manager) buildUserPrompt(recentMsgs []ChatMessage, currentMsg string) string {
	var sb strings.Builder

	if len(recentMsgs) > 0 {
		sb.WriteString("HISTORIAL RECIENTE DE LA CONVERSACION:\n")
		for _, msg := range recentMsgs {
			roleLabel := "Cliente"
			if msg.Role == "assistant" {
				roleLabel = "Asistente"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", roleLabel, msg.Content))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Cliente: %s\nAsistente: ", currentMsg))
	return sb.String()
}

var actionBlockRegex = regexp.MustCompile("(?s)```json:action\\s*(\\{.*?\\})\\s*```")

func extractActionBlock(raw string) (cleanReply string, actionJSON string) {
	matches := actionBlockRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		actionJSON = strings.TrimSpace(matches[1])
		cleanReply = strings.TrimSpace(actionBlockRegex.ReplaceAllString(raw, ""))
		return cleanReply, actionJSON
	}
	return raw, ""
}

type genericActionPayload struct {
	Action          string `json:"action"`
	CustomerName    string `json:"customer_name"`
	CustomerPhone   string `json:"customer_phone"`
	DeliveryAddress string `json:"delivery_address"`
	PaymentMethod   string `json:"payment_method"`
	Service         string `json:"service"`
	DateTime        string `json:"date_time"`
	Items           []struct {
		ID  string `json:"id"`
		Qty int    `json:"qty"`
	} `json:"items"`
}

func (m *Manager) processAction(spec *BotSpec, sessionID, actionJSON string, result *ChatResult) {
	var payload genericActionPayload
	if err := json.Unmarshal([]byte(actionJSON), &payload); err != nil {
		return
	}

	switch payload.Action {
	case "create_order":
		var orderItems []OrderItem
		var total float64

		for _, reqItem := range payload.Items {
			qty := reqItem.Qty
			if qty <= 0 {
				qty = 1
			}

			catItem := findCatalogItem(spec.Catalog, reqItem.ID)
			if catItem == nil {
				continue
			}

			subtotal := float64(qty) * catItem.Price
			total += subtotal

			orderItems = append(orderItems, OrderItem{
				CatalogItemID: catItem.ID,
				Name:          catItem.Name,
				Quantity:      qty,
				UnitPrice:     catItem.Price,
				Subtotal:      subtotal,
			})
		}

		if len(orderItems) == 0 {
			return
		}

		phone := payload.CustomerPhone
		if phone == "" {
			phone = sessionID
		}

		order := &OrderRecord{
			BotID:           spec.ID,
			CustomerPhone:   phone,
			CustomerName:    payload.CustomerName,
			Items:           orderItems,
			Total:           total,
			DeliveryAddress: payload.DeliveryAddress,
			PaymentMethod:   payload.PaymentMethod,
			Status:          "confirmed",
			CreatedAt:       time.Now().UTC(),
		}

		id, err := m.storage.SaveOrder(order)
		if err == nil {
			order.ID = id
			result.Order = order
			result.CompletedSlot = true
		}

	case "create_appointment":
		t, err := parseFlexDateTime(payload.DateTime)
		if err != nil {
			return
		}

		duration := 30
		if spec.BusinessHours != nil && spec.BusinessHours.SlotDurationMinutes > 0 {
			duration = spec.BusinessHours.SlotDurationMinutes
		}

		conflict, err := m.storage.HasAppointmentConflict(spec.ID, t, duration)
		if err == nil && conflict {
			result.Reply += "\n\n(Aviso: El turno solicitado se encuentra ocupado en nuestra agenda. Por favor indícanos otro horario disponible)."
			return
		}

		phone := payload.CustomerPhone
		if phone == "" {
			phone = sessionID
		}

		app := &AppointmentRecord{
			BotID:           spec.ID,
			CustomerPhone:   phone,
			CustomerName:    payload.CustomerName,
			Service:         payload.Service,
			StartTime:       t,
			DurationMinutes: duration,
			Status:          "scheduled",
			CreatedAt:       time.Now().UTC(),
		}

		id, err := m.storage.SaveAppointment(app)
		if err == nil {
			app.ID = id
			result.Appointment = app
			result.CompletedSlot = true
		}
	}
}

func findCatalogItem(catalog []CatalogItem, query string) *CatalogItem {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}

	for i := range catalog {
		if strings.ToLower(catalog[i].ID) == query {
			return &catalog[i]
		}
	}

	for i := range catalog {
		if strings.ToLower(catalog[i].Name) == query {
			return &catalog[i]
		}
	}

	for i := range catalog {
		if strings.Contains(strings.ToLower(catalog[i].Name), query) {
			return &catalog[i]
		}
	}

	return nil
}

func parseFlexDateTime(val string) (time.Time, error) {
	val = strings.TrimSpace(val)
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"02/01/2006 15:04",
		"02-01-2006 15:04",
	}

	for _, layout := range formats {
		if t, err := time.Parse(layout, val); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("formato de fecha/hora desconocido: %s", val)
}
