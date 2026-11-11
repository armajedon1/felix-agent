package users

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleContact Role = "contact"
	RoleBlocked Role = "blocked"
)

type User struct {
	JID       string    `json:"jid"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type VetoRule struct {
	ID            int64     `json:"id"`
	TargetContact string    `json:"target_contact"`
	Rule          string    `json:"rule"`
	CreatedAt     time.Time `json:"created_at"`
}

type Manager struct {
	dbPath string
	db     *sql.DB
	mu     sync.RWMutex
	admins map[string]bool
}

func NewManager(dbPath string, defaultAdmins []string) (*Manager, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("error abriendo base de usuarios: %w", err)
	}

	m := &Manager{
		dbPath: dbPath,
		db:     db,
		admins: make(map[string]bool),
	}

	for _, a := range defaultAdmins {
		clean := cleanJID(a)
		if clean != "" {
			m.admins[clean] = true
		}
	}

	if err := m.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return m, nil
}

func (m *Manager) initSchema() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	query := `
	CREATE TABLE IF NOT EXISTS users (
		jid TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		role TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS confidentiality_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_contact TEXT NOT NULL,
		rule TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := m.db.Exec(query)
	if err != nil {
		return fmt.Errorf("error inicializando esquema de usuarios: %w", err)
	}

	for admin := range m.admins {
		_, _ = m.db.Exec(`INSERT OR IGNORE INTO users (jid, name, role) VALUES (?, ?, ?);`, admin, "Administrador (Admin)", string(RoleAdmin))
	}

	return nil
}

func cleanJID(jid string) string {
	s := strings.TrimSpace(jid)
	s = strings.TrimPrefix(s, "+")
	if idx := strings.Index(s, "@"); idx != -1 {
		s = s[:idx]
	}
	if idx := strings.Index(s, ":"); idx != -1 {
		s = s[:idx]
	}
	return s
}

// IsAnyAdminConfigured verifica si existe al menos un administrador activo.
func (m *Manager) IsAnyAdminConfigured() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.admins) > 0 {
		return true
	}
	var count int
	_ = m.db.QueryRow(`SELECT count(*) FROM users WHERE role = 'admin';`).Scan(&count)
	return count > 0
}

// EnsureFirstAdmin registra al primer usuario como administrador si no existe ninguno.
func (m *Manager) EnsureFirstAdmin(jid, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clean := cleanJID(jid)
	if clean == "" {
		return false, fmt.Errorf("JID vacio")
	}

	var count int
	_ = m.db.QueryRow(`SELECT count(*) FROM users WHERE role = 'admin';`).Scan(&count)
	if count > 0 || len(m.admins) > 0 {
		return false, nil
	}

	if name == "" {
		name = "Administrador Principal"
	}
	_, err := m.db.Exec(`
		INSERT INTO users (jid, name, role, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(jid) DO UPDATE SET role = 'admin', name = excluded.name;
	`, clean, name, string(RoleAdmin))
	if err != nil {
		return false, err
	}

	m.admins[clean] = true
	return true, nil
}

// GetPrimaryAdmin retorna el JID del administrador principal.
func (m *Manager) GetPrimaryAdmin() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for a := range m.admins {
		return a
	}
	var jid string
	_ = m.db.QueryRow(`SELECT jid FROM users WHERE role = 'admin' ORDER BY created_at ASC LIMIT 1;`).Scan(&jid)
	return jid
}

// GetRole determina el rol asignado a un remitente (JID o telefono).
func (m *Manager) GetRole(jid string) Role {
	clean := cleanJID(jid)
	if m.admins[clean] {
		return RoleAdmin
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var role string
	err := m.db.QueryRow(`SELECT role FROM users WHERE jid = ? OR jid = ?;`, clean, jid).Scan(&role)
	if err != nil {
		return RoleBlocked
	}

	return Role(role)
}

// IsAdmin verifica si el remitente tiene permisos de administrador.
func (m *Manager) IsAdmin(jid string) bool {
	return m.GetRole(jid) == RoleAdmin
}

// AddOrUpdateUser registra o actualiza un contacto con su rol.
func (m *Manager) AddOrUpdateUser(jid, name string, role Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	clean := cleanJID(jid)
	if role == RoleAdmin {
		m.admins[clean] = true
	}
	_, err := m.db.Exec(`
		INSERT INTO users (jid, name, role, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(jid) DO UPDATE SET name = excluded.name, role = excluded.role;
	`, clean, name, string(role))
	return err
}

// ListUsers lista todos los usuarios y contactos registrados.
func (m *Manager) ListUsers() ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`SELECT jid, name, role, created_at FROM users ORDER BY role ASC, name ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.JID, &u.Name, &u.Role, &created); err == nil {
			u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
			list = append(list, u)
		}
	}
	return list, nil
}

// AddVetoRule agrega una regla de confidencialidad para un contacto o global ("*").
func (m *Manager) AddVetoRule(targetContact, rule string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	target := strings.TrimSpace(targetContact)
	if target != "*" {
		target = cleanJID(target)
	}
	_, err := m.db.Exec(`INSERT INTO confidentiality_rules (target_contact, rule) VALUES (?, ?);`, target, strings.TrimSpace(rule))
	return err
}

// GetVetoRulesForUser recupera todas las reglas de confidencialidad que aplican a un contacto.
func (m *Manager) GetVetoRulesForUser(jid, name string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clean := cleanJID(jid)
	rows, err := m.db.Query(`
		SELECT rule FROM confidentiality_rules 
		WHERE target_contact = '*' OR target_contact = ? OR target_contact = ? OR target_contact = ?;
	`, clean, jid, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var rules []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err == nil && r != "" {
			rules = append(rules, r)
		}
	}
	return rules
}

// ListVetoRules retorna todas las reglas de confidencialidad activas en el sistema.
func (m *Manager) ListVetoRules() ([]VetoRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`SELECT id, target_contact, rule, created_at FROM confidentiality_rules ORDER BY id DESC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []VetoRule
	for rows.Next() {
		var vr VetoRule
		var created string
		if err := rows.Scan(&vr.ID, &vr.TargetContact, &vr.Rule, &created); err == nil {
			vr.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
			list = append(list, vr)
		}
	}
	return list, nil
}

// DeleteVetoRule elimina una regla de confidencialidad por ID.
func (m *Manager) DeleteVetoRule(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec(`DELETE FROM confidentiality_rules WHERE id = ?;`, id)
	return err
}

func (m *Manager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}
