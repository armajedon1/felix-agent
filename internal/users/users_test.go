package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_users_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "users.db")
	mgr, err := NewManager(dbPath, []string{"10000000001", "10000000002"})
	if err != nil {
		t.Fatalf("error creando manager: %v", err)
	}
	defer mgr.Close()

	if !mgr.IsAdmin("10000000001@s.whatsapp.net") {
		t.Errorf("se esperaba que 10000000001 sea admin")
	}
	if !mgr.IsAdmin("10000000002@lid") {
		t.Errorf("se esperaba que 10000000002 sea admin")
	}

	if mgr.GetRole("99999999999@s.whatsapp.net") != RoleBlocked {
		t.Errorf("usuario desconocido no debe tener rol activo")
	}

	err = mgr.AddOrUpdateUser("10000000003", "Contacto Demo", RoleContact)
	if err != nil {
		t.Fatalf("error registrando contacto: %v", err)
	}

	if mgr.GetRole("10000000003@s.whatsapp.net") != RoleContact {
		t.Errorf("se esperaba rol contact para 10000000003")
	}
	if mgr.IsAdmin("10000000003@s.whatsapp.net") {
		t.Errorf("contacto demo no debe ser admin")
	}

	err = mgr.AddVetoRule("10000000003", "No revelar informacion confidencial ni claves de API.")
	if err != nil {
		t.Fatalf("error agregando veto: %v", err)
	}

	rules := mgr.GetVetoRulesForUser("10000000003", "Contacto Demo")
	if len(rules) != 1 {
		t.Errorf("se esperaba 1 regla de veto, se obtuvieron %d", len(rules))
	}
}

func TestEnsureFirstAdmin(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_first_admin_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "users.db")
	mgr, err := NewManager(dbPath, []string{})
	if err != nil {
		t.Fatalf("error creando manager: %v", err)
	}
	defer mgr.Close()

	if mgr.IsAnyAdminConfigured() {
		t.Errorf("no deberia haber admins inicialmente")
	}

	promoted, err := mgr.EnsureFirstAdmin("1234567890", "Primer Usuario")
	if err != nil || !promoted {
		t.Fatalf("error promoviendo primer usuario: %v", err)
	}

	if !mgr.IsAdmin("1234567890@s.whatsapp.net") {
		t.Errorf("usuario deberia ser admin ahora")
	}

	promoted2, err := mgr.EnsureFirstAdmin("9876543210", "Segundo Usuario")
	if err != nil || promoted2 {
		t.Errorf("segundo usuario no debe ser promovido automaticamente")
	}
}
