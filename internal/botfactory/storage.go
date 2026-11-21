package botfactory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Storage struct {
	dbPath  string
	dataDir string
	db      *sql.DB
	botDBs  map[string]*sql.DB
	mu      sync.RWMutex
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
		dbPath:  dbPath,
		dataDir: dir,
		db:      db,
		botDBs:  make(map[string]*sql.DB),
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

func sanitizeBotID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "..", "")
	id = strings.ReplaceAll(id, "/", "")
	id = strings.ReplaceAll(id, "\\", "")
	if id == "" {
		id = "default"
	}
	return id
}

func (s *Storage) GetBotDB(botID string) (*sql.DB, error) {
	botID = sanitizeBotID(botID)

	s.mu.RLock()
	if db, exists := s.botDBs[botID]; exists && db != nil {
		s.mu.RUnlock()
		return db, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if db, exists := s.botDBs[botID]; exists && db != nil {
		return db, nil
	}

	botDir := filepath.Join(s.dataDir, "bots", botID)
	if err := os.MkdirAll(botDir, 0755); err != nil {
		return nil, fmt.Errorf("error creando directorio para bot %s: %w", botID, err)
	}

	botDBPath := filepath.Join(botDir, "data.db")
	_, statErr := os.Stat(botDBPath)
	isNew := os.IsNotExist(statErr)

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", botDBPath)
	botDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error abriendo base de bot %s: %w", botID, err)
	}

	if err := s.initBotIndividualSchema(botDB, botID); err != nil {
		botDB.Close()
		return nil, fmt.Errorf("error inicializando esquema de bot %s: %w", botID, err)
	}

	if isNew {
		s.migrateLegacyBotData(botDB, botID)
	}

	s.botDBs[botID] = botDB
	return botDB, nil
}

func (s *Storage) initBotIndividualSchema(botDB *sql.DB, botID string) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			customer_phone TEXT,
			customer_name TEXT,
			last_message TEXT,
			message_count INTEGER DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, id ASC);`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT,
			customer_phone TEXT,
			customer_name TEXT,
			delivery_address TEXT,
			payment_method TEXT,
			total REAL NOT NULL,
			status TEXT NOT NULL,
			notes TEXT,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id INTEGER NOT NULL,
			catalog_item_id TEXT,
			name TEXT NOT NULL,
			quantity INTEGER NOT NULL,
			unit_price REAL NOT NULL,
			subtotal REAL NOT NULL,
			FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_order_items_order ON order_items(order_id);`,
		`CREATE TABLE IF NOT EXISTS appointments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT,
			customer_phone TEXT,
			customer_name TEXT,
			service TEXT NOT NULL,
			start_time DATETIME NOT NULL,
			duration_minutes INTEGER NOT NULL,
			notes TEXT,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_appointments_time ON appointments(start_time ASC);`,
	}

	for _, q := range queries {
		if _, err := botDB.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) migrateLegacyBotData(botDB *sql.DB, botID string) {
	msgRows, err := s.db.Query("SELECT session_id, role, content, created_at FROM bot_messages WHERE bot_id = ? ORDER BY id ASC", botID)
	if err == nil {
		defer msgRows.Close()
		for msgRows.Next() {
			var sid, role, content string
			var catRaw any
			if err := msgRows.Scan(&sid, &role, &content, &catRaw); err == nil {
				cat := parseSQLiteTime(catRaw)
				_, _ = botDB.Exec(`INSERT INTO sessions (session_id, customer_phone, last_message, message_count, created_at, updated_at)
					VALUES (?, ?, ?, 1, ?, ?)
					ON CONFLICT(session_id) DO UPDATE SET
						last_message = excluded.last_message,
						message_count = sessions.message_count + 1,
						updated_at = excluded.updated_at;`, sid, sid, content, cat, cat)
				_, _ = botDB.Exec(`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`, sid, role, content, cat)
			}
		}
	}

	orderRows, err := s.db.Query("SELECT id, customer_phone, customer_name, items_json, total, address, payment_method, status, created_at FROM bot_orders WHERE bot_id = ?", botID)
	if err == nil {
		defer orderRows.Close()
		for orderRows.Next() {
			var id int64
			var cphone, cname, itemsJSON, addr, pay, status string
			var total float64
			var catRaw any
			if err := orderRows.Scan(&id, &cphone, &cname, &itemsJSON, &total, &addr, &pay, &status, &catRaw); err == nil {
				cat := parseSQLiteTime(catRaw)
				_, _ = botDB.Exec(`INSERT INTO orders (id, session_id, customer_phone, customer_name, delivery_address, payment_method, total, status, created_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, cphone, cphone, cname, addr, pay, total, status, cat)
				var items []OrderItem
				if json.Unmarshal([]byte(itemsJSON), &items) == nil {
					for _, it := range items {
						_, _ = botDB.Exec(`INSERT INTO order_items (order_id, catalog_item_id, name, quantity, unit_price, subtotal)
							VALUES (?, ?, ?, ?, ?, ?)`, id, it.CatalogItemID, it.Name, it.Quantity, it.UnitPrice, it.Subtotal)
					}
				}
			}
		}
	}

	appRows, err := s.db.Query("SELECT id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at FROM bot_appointments WHERE bot_id = ?", botID)
	if err == nil {
		defer appRows.Close()
		for appRows.Next() {
			var id int64
			var cphone, cname, serv, status string
			var dur int
			var notes sql.NullString
			var startRaw, catRaw any
			if err := appRows.Scan(&id, &cphone, &cname, &serv, &startRaw, &dur, &notes, &status, &catRaw); err == nil {
				startT := parseSQLiteTime(startRaw)
				catT := parseSQLiteTime(catRaw)
				noteStr := ""
				if notes.Valid {
					noteStr = notes.String
				}
				_, _ = botDB.Exec(`INSERT INTO appointments (id, session_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, cphone, cphone, cname, serv, startT, dur, noteStr, status, catT)
			}
		}
	}
}

func (s *Storage) DeleteBot(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanID := sanitizeBotID(id)
	if botDB, ok := s.botDBs[cleanID]; ok && botDB != nil {
		_ = botDB.Close()
		delete(s.botDBs, cleanID)
	}

	botDir := filepath.Join(s.dataDir, "bots", cleanID)
	_ = os.RemoveAll(botDir)

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
	botDB, err := s.GetBotDB(msg.BotID)
	if err != nil {
		return err
	}

	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	tx, err := botDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sessQuery := `INSERT INTO sessions (session_id, customer_phone, last_message, message_count, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			last_message = excluded.last_message,
			message_count = sessions.message_count + 1,
			updated_at = excluded.updated_at;`
	if _, err := tx.Exec(sessQuery, msg.SessionID, msg.SessionID, msg.Content, msg.CreatedAt, msg.CreatedAt); err != nil {
		return err
	}

	msgQuery := `INSERT INTO messages (session_id, role, content, created_at)
		VALUES (?, ?, ?, ?);`
	res, err := tx.Exec(msgQuery, msg.SessionID, msg.Role, msg.Content, msg.CreatedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		msg.ID = id
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.mu.Lock()
	legacyQuery := `INSERT INTO bot_messages (bot_id, session_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?);`
	_, _ = s.db.Exec(legacyQuery, msg.BotID, msg.SessionID, msg.Role, msg.Content, msg.CreatedAt)
	s.mu.Unlock()

	return nil
}

func (s *Storage) GetRecentMessages(botID, sessionID string, limit int) ([]ChatMessage, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, session_id, role, content, created_at 
		FROM messages 
		WHERE session_id = ?
		ORDER BY id DESC LIMIT ?;`

	rows, err := botDB.Query(query, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var descMsgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var catRaw any
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &catRaw); err != nil {
			return nil, err
		}
		m.BotID = botID
		m.CreatedAt = parseSQLiteTime(catRaw)
		descMsgs = append(descMsgs, m)
	}

	result := make([]ChatMessage, len(descMsgs))
	for i, m := range descMsgs {
		result[len(descMsgs)-1-i] = m
	}

	return result, nil
}

func (s *Storage) ListBotSessions(botID string) ([]SessionSummary, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	query := `SELECT session_id, COALESCE(customer_name, ''), last_message, message_count, updated_at
		FROM sessions ORDER BY updated_at DESC LIMIT 100;`
	rows, err := botDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var sess SessionSummary
		var updatedRaw any
		if err := rows.Scan(&sess.SessionID, &sess.CustomerName, &sess.LastMessage, &sess.MessageCount, &updatedRaw); err != nil {
			continue
		}
		sess.BotID = botID
		sess.UpdatedAt = parseSQLiteTime(updatedRaw)
		sessions = append(sessions, sess)
	}

	return sessions, nil
}

func (s *Storage) GetBotConversation(botID, sessionID string) ([]ChatMessage, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	query := `SELECT id, session_id, role, content, created_at
		FROM messages WHERE session_id = ? ORDER BY id ASC;`
	rows, err := botDB.Query(query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var catRaw any
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &catRaw); err != nil {
			continue
		}
		m.BotID = botID
		m.CreatedAt = parseSQLiteTime(catRaw)
		msgs = append(msgs, m)
	}

	return msgs, nil
}

func (s *Storage) SaveOrder(order *OrderRecord) (int64, error) {
	botDB, err := s.GetBotDB(order.BotID)
	if err != nil {
		return 0, err
	}

	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now().UTC()
	}
	if order.Status == "" {
		order.Status = "confirmed"
	}

	tx, err := botDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	orderQuery := `INSERT INTO orders (session_id, customer_phone, customer_name, delivery_address, payment_method, total, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	res, err := tx.Exec(orderQuery, order.CustomerPhone, order.CustomerPhone, order.CustomerName, order.DeliveryAddress, order.PaymentMethod, order.Total, order.Status, order.CreatedAt)
	if err != nil {
		return 0, err
	}

	orderID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	itemQuery := `INSERT INTO order_items (order_id, catalog_item_id, name, quantity, unit_price, subtotal)
		VALUES (?, ?, ?, ?, ?, ?);`
	for _, it := range order.Items {
		if _, err := tx.Exec(itemQuery, orderID, it.CatalogItemID, it.Name, it.Quantity, it.UnitPrice, it.Subtotal); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	order.ID = orderID

	s.mu.Lock()
	itemsRaw, _ := json.Marshal(order.Items)
	legacyQuery := `INSERT INTO bot_orders (id, bot_id, customer_phone, customer_name, items_json, total, address, payment_method, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	_, _ = s.db.Exec(legacyQuery, orderID, order.BotID, order.CustomerPhone, order.CustomerName, string(itemsRaw), order.Total, order.DeliveryAddress, order.PaymentMethod, order.Status, order.CreatedAt)
	s.mu.Unlock()

	return orderID, nil
}

func (s *Storage) ListOrders(botID string, limit int) ([]OrderRecord, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, session_id, customer_phone, customer_name, delivery_address, payment_method, total, status, created_at
		FROM orders ORDER BY id DESC LIMIT ?;`
	rows, err := botDB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []OrderRecord
	for rows.Next() {
		var o OrderRecord
		var sessID sql.NullString
		var catRaw any
		if err := rows.Scan(&o.ID, &sessID, &o.CustomerPhone, &o.CustomerName, &o.DeliveryAddress, &o.PaymentMethod, &o.Total, &o.Status, &catRaw); err != nil {
			return nil, err
		}
		o.BotID = botID
		o.CreatedAt = parseSQLiteTime(catRaw)
		orders = append(orders, o)
	}
	rows.Close()

	itemQuery := `SELECT catalog_item_id, name, quantity, unit_price, subtotal FROM order_items WHERE order_id = ?;`
	for i := range orders {
		itemRows, err := botDB.Query(itemQuery, orders[i].ID)
		if err != nil {
			continue
		}
		for itemRows.Next() {
			var it OrderItem
			_ = itemRows.Scan(&it.CatalogItemID, &it.Name, &it.Quantity, &it.UnitPrice, &it.Subtotal)
			orders[i].Items = append(orders[i].Items, it)
		}
		itemRows.Close()
	}

	return orders, nil
}

func (s *Storage) SaveAppointment(app *AppointmentRecord) (int64, error) {
	botDB, err := s.GetBotDB(app.BotID)
	if err != nil {
		return 0, err
	}

	if app.CreatedAt.IsZero() {
		app.CreatedAt = time.Now().UTC()
	}
	if app.Status == "" {
		app.Status = "scheduled"
	}
	if app.DurationMinutes <= 0 {
		app.DurationMinutes = 30
	}

	query := `INSERT INTO appointments (session_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	res, err := botDB.Exec(query, app.CustomerPhone, app.CustomerPhone, app.CustomerName, app.Service, app.StartTime, app.DurationMinutes, app.Notes, app.Status, app.CreatedAt)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	app.ID = id

	s.mu.Lock()
	legacyQuery := `INSERT INTO bot_appointments (id, bot_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	_, _ = s.db.Exec(legacyQuery, id, app.BotID, app.CustomerPhone, app.CustomerName, app.Service, app.StartTime, app.DurationMinutes, app.Notes, app.Status, app.CreatedAt)
	s.mu.Unlock()

	return id, nil
}

func (s *Storage) ListAppointments(botID string, limit int) ([]AppointmentRecord, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, session_id, customer_phone, customer_name, service, start_time, duration_minutes, notes, status, created_at
		FROM appointments ORDER BY start_time ASC LIMIT ?;`
	rows, err := botDB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AppointmentRecord
	for rows.Next() {
		var a AppointmentRecord
		var sessID, notes sql.NullString
		var startRaw, catRaw any
		if err := rows.Scan(&a.ID, &sessID, &a.CustomerPhone, &a.CustomerName, &a.Service, &startRaw, &a.DurationMinutes, &notes, &a.Status, &catRaw); err != nil {
			continue
		}
		a.BotID = botID
		a.StartTime = parseSQLiteTime(startRaw)
		a.CreatedAt = parseSQLiteTime(catRaw)
		if notes.Valid {
			a.Notes = notes.String
		}
		list = append(list, a)
	}

	return list, nil
}

func (s *Storage) HasAppointmentConflict(botID string, startTime time.Time, durationMinutes int) (bool, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return false, err
	}

	endTime := startTime.Add(time.Duration(durationMinutes) * time.Minute)
	searchWindowStart := startTime.Add(-2 * time.Hour)
	searchWindowEnd := endTime.Add(2 * time.Hour)

	query := `SELECT id, start_time, duration_minutes 
		FROM appointments 
		WHERE status = 'scheduled'
		AND start_time >= ? AND start_time <= ?;`
	rows, err := botDB.Query(query, searchWindowStart, searchWindowEnd)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var startRaw any
		var existDuration int
		if err := rows.Scan(&id, &startRaw, &existDuration); err != nil {
			return false, err
		}
		existStart := parseSQLiteTime(startRaw)
		existEnd := existStart.Add(time.Duration(existDuration) * time.Minute)
		if startTime.Before(existEnd) && endTime.After(existStart) {
			return true, nil
		}
	}

	return false, nil
}

func (s *Storage) GetStats(botID string) (*BotStats, error) {
	botDB, err := s.GetBotDB(botID)
	if err == nil {
		stats := &BotStats{}
		_ = botDB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&stats.TotalMessages)
		_ = botDB.QueryRow("SELECT COUNT(*) FROM orders").Scan(&stats.TotalOrders)
		_ = botDB.QueryRow("SELECT COUNT(*) FROM appointments").Scan(&stats.TotalAppointments)
		return stats, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &BotStats{}
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_messages WHERE bot_id = ?", botID).Scan(&stats.TotalMessages)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_orders WHERE bot_id = ?", botID).Scan(&stats.TotalOrders)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM bot_appointments WHERE bot_id = ?", botID).Scan(&stats.TotalAppointments)

	return stats, nil
}

func (s *Storage) GetBotDBInfo(botID string) (*BotDBInfo, error) {
	botDB, err := s.GetBotDB(botID)
	if err != nil {
		return nil, err
	}

	cleanID := sanitizeBotID(botID)
	botDir := filepath.Join(s.dataDir, "bots", cleanID)
	botDBPath := filepath.Join(botDir, "data.db")

	info := &BotDBInfo{
		BotID:  cleanID,
		DBPath: botDBPath,
	}

	if stat, err := os.Stat(botDBPath); err == nil {
		info.FileSizeBytes = stat.Size()
	}

	_ = botDB.QueryRow("PRAGMA journal_mode;").Scan(&info.JournalMode)

	tableRows, err := botDB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name ASC;")
	if err == nil {
		defer tableRows.Close()
		for tableRows.Next() {
			var name string
			if err := tableRows.Scan(&name); err == nil {
				info.Tables = append(info.Tables, name)
			}
		}
	}

	_ = botDB.QueryRow("SELECT COUNT(*) FROM orders;").Scan(&info.TotalOrders)
	_ = botDB.QueryRow("SELECT COUNT(*) FROM messages;").Scan(&info.TotalMessages)
	_ = botDB.QueryRow("SELECT COUNT(*) FROM appointments;").Scan(&info.TotalAppointments)

	ddlRows, err := botDB.Query("SELECT sql FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';")
	if err == nil {
		defer ddlRows.Close()
		var ddlBuilder strings.Builder
		for ddlRows.Next() {
			var sqlStmt sql.NullString
			if err := ddlRows.Scan(&sqlStmt); err == nil && sqlStmt.Valid {
				ddlBuilder.WriteString(sqlStmt.String)
				ddlBuilder.WriteString(";\n\n")
			}
		}
		info.SchemaDDL = ddlBuilder.String()
	}

	return info, nil
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

	rows, err := s.db.Query("SELECT id, name, status, spec_json, updated_at FROM bots ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BotNumberInfo
	for rows.Next() {
		var id, name, status, raw string
		var updatedAt time.Time
		if err := rows.Scan(&id, &name, &status, &raw, &updatedAt); err != nil {
			continue
		}
		var spec BotSpec
		_ = json.Unmarshal([]byte(raw), &spec)

		info := BotNumberInfo{
			BotID:            id,
			BotName:          name,
			IsActive:         (status == "active" || spec.IsActive),
			LastSeen:         updatedAt,
			ChannelType:      spec.ChannelType,
			TelegramUsername: spec.TelegramUsername,
			PairingCode:      spec.PairingCode,
		}

		if info.ChannelType == "" {
			if spec.PhoneNumber != "" {
				info.ChannelType = "whatsapp"
			} else if spec.TelegramToken != "" {
				info.ChannelType = "telegram"
			} else {
				info.ChannelType = "web"
			}
		}

		if info.ChannelType == "telegram" {
			if info.TelegramUsername != "" {
				info.PhoneNumber = info.TelegramUsername
			} else {
				info.PhoneNumber = "Token Configurado"
			}
		} else if info.ChannelType == "whatsapp" {
			info.PhoneNumber = spec.PhoneNumber
		} else {
			info.PhoneNumber = "Consola Web / CLI"
		}

		result = append(result, info)
	}

	return result, nil
}

func (s *Storage) AssignWhatsApp(botID, phoneNumber string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		return fmt.Errorf("bot '%s' no encontrado: %w", botID, err)
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}

	isActive := (spec.Status == "active" || spec.IsActive)
	if isActive && phoneNumber != "" {
		var otherBotID string
		err := s.db.QueryRow("SELECT bot_id FROM bot_numbers WHERE phone_number = ? AND is_active = 1 AND bot_id != ?", phoneNumber, botID).Scan(&otherBotID)
		if err == nil && otherBotID != "" {
			return fmt.Errorf("colision de numero: el numero '%s' ya esta asignado y activo en el bot '%s'. Debe desactivar el otro bot antes de asignarlo", phoneNumber, otherBotID)
		}
	}

	spec.PhoneNumber = phoneNumber
	spec.ChannelType = "whatsapp"
	spec.UpdatedAt = time.Now().UTC()

	newRaw, _ := json.Marshal(spec)
	if _, err := s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(newRaw), spec.UpdatedAt, botID); err != nil {
		return err
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
	_, _ = s.db.Exec(numQuery, phoneNumber, botID, numActive)
	return nil
}

func (s *Storage) AssignTelegram(botID, token, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		return fmt.Errorf("bot '%s' no encontrado: %w", botID, err)
	}

	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}

	spec.ChannelType = "telegram"
	spec.TelegramToken = token
	spec.TelegramUsername = username
	spec.UpdatedAt = time.Now().UTC()

	newRaw, _ := json.Marshal(spec)
	if _, err := s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(newRaw), spec.UpdatedAt, botID); err != nil {
		return err
	}
	return nil
}

func (s *Storage) SetBotPairingCode(botID, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow("SELECT spec_json FROM bots WHERE id = ?", botID).Scan(&raw); err != nil {
		return err
	}
	var spec BotSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}
	spec.PairingCode = code
	spec.UpdatedAt = time.Now().UTC()
	newRaw, _ := json.Marshal(spec)
	_, err := s.db.Exec("UPDATE bots SET spec_json = ?, updated_at = ? WHERE id = ?", string(newRaw), spec.UpdatedAt, botID)
	return err
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

	for _, botDB := range s.botDBs {
		if botDB != nil {
			_ = botDB.Close()
		}
	}
	s.botDBs = make(map[string]*sql.DB)

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
