package botfactory

import (
	"time"
)

type BotType string

const (
	TypeOrderTaking        BotType = "order_taking"
	TypeAppointmentBooking BotType = "appointment_booking"
	TypeFAQSupport         BotType = "faq_support"
	TypeCustom             BotType = "custom"
)

type CatalogItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	Description string  `json:"description,omitempty"`
	Available   bool    `json:"available"`
}

type FAQItem struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Keywords []string `json:"keywords,omitempty"`
}

type BusinessHours struct {
	Days                []string `json:"days"`
	OpenTime            string   `json:"open_time"`
	CloseTime           string   `json:"close_time"`
	SlotDurationMinutes int      `json:"slot_duration_minutes"`
}

type BotSpec struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Type            BotType        `json:"type"`
	Description     string         `json:"description"`
	SystemPrompt    string         `json:"system_prompt"`
	Catalog         []CatalogItem  `json:"catalog,omitempty"`
	FAQ             []FAQItem      `json:"faq,omitempty"`
	BusinessHours   *BusinessHours `json:"business_hours,omitempty"`
	RequiredSlots   []string       `json:"required_slots,omitempty"`
	StrictGrounding bool           `json:"strict_grounding"`
	FallbackMessage string         `json:"fallback_message,omitempty"`
	Status          string         `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type OrderItem struct {
	CatalogItemID string  `json:"catalog_item_id"`
	Name          string  `json:"name"`
	Quantity      int     `json:"quantity"`
	UnitPrice     float64 `json:"unit_price"`
	Subtotal      float64 `json:"subtotal"`
}

type OrderRecord struct {
	ID              int64       `json:"id"`
	BotID           string      `json:"bot_id"`
	CustomerPhone   string      `json:"customer_phone"`
	CustomerName    string      `json:"customer_name"`
	Items           []OrderItem `json:"items"`
	Total           float64     `json:"total"`
	DeliveryAddress string      `json:"delivery_address"`
	PaymentMethod   string      `json:"payment_method"`
	Status          string      `json:"status"`
	CreatedAt       time.Time   `json:"created_at"`
}

type AppointmentRecord struct {
	ID              int64     `json:"id"`
	BotID           string    `json:"bot_id"`
	CustomerPhone   string    `json:"customer_phone"`
	CustomerName    string    `json:"customer_name"`
	Service         string    `json:"service"`
	StartTime       time.Time `json:"start_time"`
	DurationMinutes int       `json:"duration_minutes"`
	Notes           string    `json:"notes,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type ChatMessage struct {
	ID        int64     `json:"id"`
	BotID     string    `json:"bot_id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatResult struct {
	Reply         string             `json:"reply"`
	Order         *OrderRecord       `json:"order,omitempty"`
	Appointment   *AppointmentRecord `json:"appointment,omitempty"`
	CompletedSlot bool               `json:"completed_slot"`
}

type BotStats struct {
	TotalMessages     int64 `json:"total_messages"`
	TotalOrders       int64 `json:"total_orders"`
	TotalAppointments int64 `json:"total_appointments"`
}
