package botfactory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Storage struct {
	dbPath string
	db     *sql.DB
	mu     sync.RWMutex
}

func NewStorage(dbPath string) (*Storage, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creando directorio para storage de bots: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error abriendo base de bots: %w", err)
	}

	s := &Storage{
		dbPath: dbPath,
		db:     db,
	}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("error inicializando esquema de bots: %w", err)
	}

	return s, nil
}

func (s *Storage) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS bots (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			description TEXT,
			spec_json TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS bot_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bot_messages_lookup 
			ON bot_messages(bot_id, session_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS bot_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_id TEXT NOT NULL,
			customer_phone TEXT,
			customer_name TEXT,
			items_json TEXT NOT NULL,
			total REAL NOT NULL,
			address TEXT,
			payment_method TEXT,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bot_orders_lookup 
			ON bot_orders(bot_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS bot_appointments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_id TEXT NOT NULL,
			customer_phone TEXT,
			customer_name TEXT,
			service TEXT NOT NULL,
			start_time DATETIME NOT NULL,
			duration_minutes INTEGER NOT NULL,
			notes TEXT,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bot_appointments_lookup 
			ON bot_appointments(bot_id, start_time);`,
		`CREATE TABLE IF NOT EXISTS bot_numbers (
			phone_number TEXT NOT NULL,
			bot_id TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			carrier_info TEXT,
			last_seen DATETIME,
			PRIMARY KEY (phone_number, bot_id)
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_active_phone_number 
			ON bot_numbers(phone_number) 
			WHERE is_active = 1;`,
		`CREATE TABLE IF NOT EXISTS bot_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_id TEXT NOT NULL,
			level TEXT NOT NULL,
			direction TEXT,
			sender_number TEXT,
			message TEXT NOT NULL,
			latency_ms INTEGER DEFAULT 0,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bot_logs_bot_id_created 
			ON bot_logs(bot_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_bot_logs_level 
			ON bot_logs(level, created_at DESC);`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

func (s *Storage) SaveBot(spec *BotSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now().UTC()
	}
	spec.UpdatedAt = time.Now().UTC()
	if spec.Status == "" {
		spec.Status = "active"
	}
	spec.IsActive = (spec.Status == "active")

	if spec.PhoneNumber != "" && spec.IsActive {
		var otherBotID string
		err := s.db.QueryRow("SELECT bot_id FROM bot_numbers WHERE phone_number = ? AND is_active = 1 AND bot_id != ?", spec.PhoneNumber, spec.ID).Scan(&otherBotID)
		if err == nil && otherBotID != "" {
			return fmt.Errorf("colision de numero: el numero '%s' ya esta activo en el bot '%s'. Debe desactivar el otro bot antes de activarlo en este", spec.PhoneNumber, otherBotID)
		}
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("error serializando spec de bot: %w", err)
	}

	query := `INSERT INTO bots (id, name, type, description, spec_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			description = excluded.description,
			spec_json = excluded.spec_json,
			status = excluded.status,
			updated_at = excluded.updated_at;`

	if _, err := s.db.Exec(query, spec.ID, spec.Name, string(spec.Type), spec.Description, string(raw), spec.Status, spec.CreatedAt, spec.UpdatedAt); err != nil {
		return err
	}

	if spec.PhoneNumber != "" {
		numActive := 0
		if spec.IsActive {
			numActive = 1
		}
		numQuery := `INSERT INTO bot_numbers (phone_number, bot_id, is_active, last_seen)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(phone_number, bot_id) DO UPDATE SET
				is_active = excluded.is_active,
				last_seen = CURRENT_TIMESTAMP;`
		if _, err := s.db.Exec(numQuery, spec.PhoneNumber, spec.ID, numActive); err != nil {
			return err
		}
	}

	return nil
}

func (s *Storage) GetBot(id string) (*BotSpec, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT spec_json FROM bots WHERE id = ?;`
	row := s.db.QueryRow(query, id)

	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("bot con id '%s' no encontrado", id)
		}
		return nil, err
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, fmt.Errorf("error deserializando spec de bot: %w", err)
	}

	return &spec, nil
}

func (s *Storage) ListBots() ([]*BotSpec, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT spec_json FROM bots ORDER BY updated_at DESC;`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*BotSpec
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var spec BotSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			continue
		}
		result = append(result, &spec)
	}

	return result, nil
}

func (s *Storage) DeleteBot(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM bot_messages WHERE bot_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM bot_orders WHERE bot_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM bot_appointments WHERE bot_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM bot_numbers WHERE bot_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM bot_logs WHERE bot_id = ?", id); err != nil {
		return err
	}
	res, err := tx.Exec("DELETE FROM bots WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("bot '%s' no encontrado", id)
	}

	return tx.Commit()
}

func (s *Storage) SaveMessage(msg *ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	query := `INSERT INTO bot_messages (bot_id, session_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?);`
	res, err := s.db.Exec(query, msg.BotID, msg.SessionID, msg.Role, msg.Content, msg.CreatedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		msg.ID = id
	}
	return nil
}

func (s *Storage) GetRecentMessages(botID, sessionID string, limit int) ([]ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, bot_id, session_id, role, content, created_at 
		FROM bot_messages 
		WHERE bot_id = ? AND session_id = ?
		ORDER BY id DESC LIMIT ?;`

	rows, err := s.db.Query(query, botID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var descMsgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.BotID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		descMsgs = append(descMsgs, m)
	}

	result := make([]ChatMessage, len(descMsgs))
	for i, m := range descMsgs {
		result[len(descMsgs)-1-i] = m
	}

	return result, nil
}

func (s *Storage) SaveOrder(order *OrderRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now().UTC()
	}
	if order.Status == "" {
		order.Status = "confirmed"
	}

	itemsRaw, err := json.Marshal(order.Items)
	if err != nil {
		return 0, fmt.Errorf("error serializando items de orden: %w", err)
	}

	query := `INSERT INTO bot_orders (bot_id, customer_phone, customer_name, items_json, total, address, payment_method, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`

	res, err := s.db.Exec(query, order.BotID, order.CustomerPhone, order.CustomerName, string(itemsRaw), order.Total, order.DeliveryAddress, order.PaymentMethod, order.Status, order.CreatedAt)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	order.ID = id
	return id, nil
}

func (s *Storage) ListOrders(botID string, limit int) ([]OrderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, bot_id, customer_phone, customer_name, items_json, total, address, payment_method, status, created_at
		FROM bot_orders
		WHERE bot_id = ?
		ORDER BY id DESC LIMIT ?;`

	rows, err := s.db.Query(query, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []OrderRecord
	for rows.Next() {
		var o OrderRecord
		var itemsRaw string
		if err := rows.Scan(&o.ID, &o.BotID, &o.CustomerPhone, &o.CustomerName, &itemsRaw, &o.Total, &o.DeliveryAddress, &o.PaymentMethod, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(itemsRaw), &o.Items)
		orders = append(orders, o)
	}

	return orders, nil
}

func (s *Storage) SaveAppointment(app *AppointmentRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if app.CreatedAt.IsZero() {
		app.CreatedAt = time.Now().UTC()
	}
	if app.Status == "" {
		app.Status = "scheduled"
	}
	if app.DurationMinutes <= 0 {
		app.DurationMinutes = 30
	}

	query := `INSERT INTO bot_appointments (bot_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`

	res, err := s.db.Exec(query, app.BotID, app.CustomerPhone, app.CustomerName, app.Service, app.StartTime, app.DurationMinutes, app.Notes, app.Status, app.CreatedAt)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	app.ID = id
	return id, nil
}

func (s *Storage) ListAppointments(botID string, limit int) ([]AppointmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, bot_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at
		FROM bot_appointments
		WHERE bot_id = ?
		ORDER BY start_time ASC LIMIT ?;`

	rows, err := s.db.Query(query, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AppointmentRecord
	for rows.Next() {
		var a AppointmentRecord
		var notes sql.NullString
		if err := rows.Scan(&a.ID, &a.BotID, &a.CustomerPhone, &a.CustomerName, &a.Service, &a.StartTime, &a.DurationMinutes, &notes, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		if notes.Valid {
			a.Notes = notes.String
		}
		list = append(list, a)
	}

	return list, nil
}

func (s *Storage) HasAppointmentConflict(botID string, startTime time.Time, durationMinutes int) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	endTime := startTime.Add(time.Duration(durationMinutes) * time.Minute)

	query := `SELECT id, start_time, duration_minutes 
		FROM bot_appointments 
		WHERE bot_id = ? AND status = 'scheduled'
		AND start_time >= ? AND start_time <= ?;`

	searchWindowStart := startTime.Add(-2 * time.Hour)
	searchWindowEnd := endTime.Add(2 * time.Hour)

	rows, err := s.db.Query(query, botID, searchWindowStart, searchWindowEnd)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var existStart time.Time
		var existDuration int
		if err := rows.Scan(&id, &existStart, &existDuration); err != nil {
			return false, err
		}
		existEnd := existStart.Add(time.Duration(existDuration) * time.Minute)

		if startTime.Before(existEnd) && endTime.After(existStart) {
			return true, nil
		}
	}

	return false, nil
}

func (s *Storage) GetStats(botID string) (*BotStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &BotStats{}
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_messages WHERE bot_id = ?", botID).Scan(&stats.TotalMessages)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_orders WHERE bot_id = ?", botID).Scan(&stats.TotalOrders)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_appointments WHERE bot_id = ?", botID).Scan(&stats.TotalAppointments)

	return stats, nil
}

func (s *Storage) AssignNumber(botID, phoneNumber string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("bot con id '%s' no encontrado", botID)
		}
		return err
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}

	isActive := (spec.Status == "active" || spec.IsActive)
	if isActive {
		var otherBotID string
		err := s.db.QueryRow("SELECT bot_id FROM bot_numbers WHERE phone_number = ? AND is_active = 1 AND bot_id != ?", phoneNumber, botID).Scan(&otherBotID)
		if err == nil && otherBotID != "" {
			return fmt.Errorf("colision de numero: el numero '%s' ya esta asignado y activo en el bot '%s'. Debe desactivar el otro bot antes de asignarlo", phoneNumber, otherBotID)
		}
	}

	numActive := 0
	if isActive {
		numActive = 1
	}

	numQuery := `INSERT INTO bot_numbers (phone_number, bot_id, is_active, last_seen)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(phone_number, bot_id) DO UPDATE SET
			is_active = excluded.is_active,
			last_seen = CURRENT_TIMESTAMP;`
	if _, err := s.db.Exec(numQuery, phoneNumber, botID, numActive); err != nil {
		return err
	}

	spec.PhoneNumber = phoneNumber
	spec.UpdatedAt = time.Now().UTC()
	updatedRaw, err := json.Marshal(&spec)
	if err != nil {
		return err
	}

	_, err = s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(updatedRaw), spec.UpdatedAt, botID)
	return err
}

func (s *Storage) ReleaseNumber(botID, phoneNumber string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec("DELETE FROM bot_numbers WHERE bot_id = ? AND phone_number = ?", botID, phoneNumber); err != nil {
		return err
	}

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err == nil {
		var spec BotSpec
		if err := json.Unmarshal([]byte(raw), &spec); err == nil {
			if spec.PhoneNumber == phoneNumber {
				spec.PhoneNumber = ""
				spec.UpdatedAt = time.Now().UTC()
				if updatedRaw, err := json.Marshal(&spec); err == nil {
					_, _ = s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(updatedRaw), spec.UpdatedAt, botID)
				}
			}
		}
	}
	return nil
}

func (s *Storage) SetBotActive(botID string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("bot con id '%s' no encontrado", botID)
		}
		return err
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}

	if active {
		if spec.PhoneNumber != "" {
			var otherBotID string
			err := s.db.QueryRow("SELECT bot_id FROM bot_numbers WHERE phone_number = ? AND is_active = 1 AND bot_id != ?", spec.PhoneNumber, botID).Scan(&otherBotID)
			if err == nil && otherBotID != "" {
				return fmt.Errorf("colision de numero: el numero '%s' asignado a este bot ya esta activo en el bot '%s'. Debe desactivar el otro bot primero", spec.PhoneNumber, otherBotID)
			}
		}
		spec.Status = "active"
		spec.IsActive = true
		if _, err := s.db.Exec("UPDATE bot_numbers SET is_active = 1 WHERE bot_id = ?", botID); err != nil {
			return err
		}
	} else {
		spec.Status = "inactive"
		spec.IsActive = false
		if _, err := s.db.Exec("UPDATE bot_numbers SET is_active = 0 WHERE bot_id = ?", botID); err != nil {
			return err
		}
	}

	spec.UpdatedAt = time.Now().UTC()
	updatedRaw, err := json.Marshal(&spec)
	if err != nil {
		return err
	}

	_, err = s.db.Exec("UPDATE bots SET status = ?, spec_json = ?, updated_at = ? WHERE id = ?", spec.Status, string(updatedRaw), spec.UpdatedAt, botID)
	return err
}

func parseSQLiteTime(raw any) time.Time {
	if raw == nil {
		return time.Now().UTC()
	}
	if t, ok := raw.(time.Time); ok {
		return t
	}
	if s, ok := raw.(string); ok {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed
		}
		if parsed, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
			return parsed
		}
		if parsed, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
			return parsed
		}
	}
	return time.Now().UTC()
}

func (s *Storage) ListNumbers() ([]BotNumberInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT n.phone_number, n.bot_id, COALESCE(b.name, 'Sin asignar'), n.is_active, COALESCE(n.last_seen, CURRENT_TIMESTAMP)
		FROM bot_numbers n
		LEFT JOIN bots b ON n.bot_id = b.id
		ORDER BY n.phone_number ASC;`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BotNumberInfo
	for rows.Next() {
		var info BotNumberInfo
		var activeInt int
		var lastSeenRaw any
		if err := rows.Scan(&info.PhoneNumber, &info.BotID, &info.BotName, &activeInt, &lastSeenRaw); err != nil {
			continue
		}
		info.IsActive = (activeInt == 1)
		info.LastSeen = parseSQLiteTime(lastSeenRaw)
		result = append(result, info)
	}

	return result, nil
}

func (s *Storage) SetAvatar(botID, avatarPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("bot con id '%s' no encontrado", botID)
		}
		return err
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}

	spec.AvatarPath = avatarPath
	spec.UpdatedAt = time.Now().UTC()
	updatedRaw, err := json.Marshal(&spec)
	if err != nil {
		return err
	}

	_, err = s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(updatedRaw), spec.UpdatedAt, botID)
	return err
}

func (s *Storage) LogEvent(entry *BotLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Level == "" {
		entry.Level = "INFO"
	}

	query := `INSERT INTO bot_logs (bot_id, level, direction, sender_number, message, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);`

	res, err := s.db.Exec(query, entry.BotID, entry.Level, entry.Direction, entry.SenderNumber, entry.Message, entry.LatencyMs, entry.CreatedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		entry.ID = id
	}
	return nil
}

func (s *Storage) GetBotLogs(botID string, limit int, level string) ([]BotLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var query string
	var rows *sql.Rows
	var err error

	if level != "" {
		query = `SELECT l.id, l.bot_id, COALESCE(b.name, l.bot_id), l.level, COALESCE(l.direction, ''), COALESCE(l.sender_number, ''), l.message, l.latency_ms, l.created_at
			FROM bot_logs l
			LEFT JOIN bots b ON l.bot_id = b.id
			WHERE l.bot_id = ? AND UPPER(l.level) = UPPER(?)
			ORDER BY l.id DESC LIMIT ?;`
		rows, err = s.db.Query(query, botID, level, limit)
	} else {
		query = `SELECT l.id, l.bot_id, COALESCE(b.name, l.bot_id), l.level, COALESCE(l.direction, ''), COALESCE(l.sender_number, ''), l.message, l.latency_ms, l.created_at
			FROM bot_logs l
			LEFT JOIN bots b ON l.bot_id = b.id
			WHERE l.bot_id = ?
			ORDER BY l.id DESC LIMIT ?;`
		rows, err = s.db.Query(query, botID, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BotLogEntry
	for rows.Next() {
		var item BotLogEntry
		var createdRaw any
		if err := rows.Scan(&item.ID, &item.BotID, &item.BotName, &item.Level, &item.Direction, &item.SenderNumber, &item.Message, &item.LatencyMs, &createdRaw); err != nil {
			continue
		}
		item.CreatedAt = parseSQLiteTime(createdRaw)
		result = append(result, item)
	}
	return result, nil
}

func (s *Storage) GetAllBotLogs(limit int, level string) ([]BotLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var query string
	var rows *sql.Rows
	var err error

	if level != "" {
		query = `SELECT l.id, l.bot_id, COALESCE(b.name, l.bot_id), l.level, COALESCE(l.direction, ''), COALESCE(l.sender_number, ''), l.message, l.latency_ms, l.created_at
			FROM bot_logs l
			LEFT JOIN bots b ON l.bot_id = b.id
			WHERE UPPER(l.level) = UPPER(?)
			ORDER BY l.id DESC LIMIT ?;`
		rows, err = s.db.Query(query, level, limit)
	} else {
		query = `SELECT l.id, l.bot_id, COALESCE(b.name, l.bot_id), l.level, COALESCE(l.direction, ''), COALESCE(l.sender_number, ''), l.message, l.latency_ms, l.created_at
			FROM bot_logs l
			LEFT JOIN bots b ON l.bot_id = b.id
			ORDER BY l.id DESC LIMIT ?;`
		rows, err = s.db.Query(query, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BotLogEntry
	for rows.Next() {
		var item BotLogEntry
		var createdRaw any
		if err := rows.Scan(&item.ID, &item.BotID, &item.BotName, &item.Level, &item.Direction, &item.SenderNumber, &item.Message, &item.LatencyMs, &createdRaw); err != nil {
			continue
		}
		item.CreatedAt = parseSQLiteTime(createdRaw)
		result = append(result, item)
	}
	return result, nil
}

func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
