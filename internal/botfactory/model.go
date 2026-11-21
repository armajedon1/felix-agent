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
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Nicknames         []string          `json:"nicknames,omitempty"`
	Type              BotType           `json:"type"`
	Description       string            `json:"description"`
	Personality       string            `json:"personality,omitempty"`
	SystemPrompt      string            `json:"system_prompt"`
	Catalog           []CatalogItem     `json:"catalog,omitempty"`
	FAQ               []FAQItem         `json:"faq,omitempty"`
	BusinessHours     *BusinessHours    `json:"business_hours,omitempty"`
	RequiredSlots     []string          `json:"required_slots,omitempty"`
	StrictGrounding   bool              `json:"strict_grounding"`
	FallbackMessage   string            `json:"fallback_message,omitempty"`
	AllowedUsers      []string          `json:"allowed_users,omitempty"`
	AllowedGroups     []string          `json:"allowed_groups,omitempty"`
	AllowGroups       bool              `json:"allow_groups,omitempty"`
	AllowAllUsers     bool              `json:"allow_all_users,omitempty"`
	GroupAliases      map[string]string `json:"group_aliases,omitempty"`
	Model             string            `json:"model,omitempty"`
	APIBase           string            `json:"api_base,omitempty"`
	APIKey            string            `json:"api_key,omitempty"`
	Port              int               `json:"port,omitempty"`
	WebhookURL        string            `json:"webhook_url,omitempty"`
	ReplicationSource string            `json:"replication_source,omitempty"`
	PhoneNumber       string            `json:"phone_number,omitempty"`
	ChannelType       string            `json:"channel_type,omitempty"`
	TelegramToken     string            `json:"telegram_token,omitempty"`
	TelegramUsername  string            `json:"telegram_username,omitempty"`
	PairingCode       string            `json:"pairing_code,omitempty"`
	AvatarPath        string            `json:"avatar_path,omitempty"`
	DBSchema          string            `json:"db_schema,omitempty"`
	IsActive          bool              `json:"is_active"`
	Status            string            `json:"status"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type BotNumberInfo struct {
	PhoneNumber      string    `json:"phone_number"`
	BotID            string    `json:"bot_id"`
	BotName          string    `json:"bot_name"`
	ChannelType      string    `json:"channel_type"`
	TelegramUsername string    `json:"telegram_username,omitempty"`
	PairingCode      string    `json:"pairing_code,omitempty"`
	IsActive         bool      `json:"is_active"`
	LastSeen         time.Time `json:"last_seen,omitempty"`
}

type BotLogEntry struct {
	ID           int64     `json:"id"`
	BotID        string    `json:"bot_id"`
	BotName      string    `json:"bot_name,omitempty"`
	Level        string    `json:"level"`
	Direction    string    `json:"direction,omitempty"`
	SenderNumber string    `json:"sender_number,omitempty"`
	Message      string    `json:"message"`
	LatencyMs    int64     `json:"latency_ms,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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

type SessionSummary struct {
	BotID        string    `json:"bot_id"`
	SessionID    string    `json:"session_id"`
	CustomerName string    `json:"customer_name,omitempty"`
	LastMessage  string    `json:"last_message"`
	MessageCount int       `json:"message_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type BotDBInfo struct {
	BotID             string   `json:"bot_id"`
	DBPath            string   `json:"db_path"`
	FileSizeBytes     int64    `json:"file_size_bytes"`
	JournalMode       string   `json:"journal_mode"`
	Tables            []string `json:"tables"`
	TotalOrders       int64    `json:"total_orders"`
	TotalMessages     int64    `json:"total_messages"`
	TotalAppointments int64    `json:"total_appointments"`
	SchemaDDL         string   `json:"schema_ddl,omitempty"`
}
