package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"felix-go/internal/users"
)

// UsersManagerTool permite al administrador gestionar el control de acceso,
// roles de contactos y directivas de veto de informacion confidencial.
type UsersManagerTool struct {
	UserMgr *users.Manager
}

func (t *UsersManagerTool) Name() string { return "users_manager" }
func (t *UsersManagerTool) Description() string {
	return "Gestiona la lista de usuarios autorizados, roles (admin, contact, blocked) y directivas de confidencialidad y veto de informacion (ej. prohibir que el asistente hable de determinados proyectos o datos ante un contacto especifico)."
}

func (t *UsersManagerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_users", "add_contact", "block_user", "list_veto", "add_veto", "delete_veto"},
				"description": "Accion a realizar sobre el sistema de usuarios o confidencialidad.",
			},
			"jid": map[string]any{
				"type":        "string",
				"description": "JID o numero de telefono del contacto (para add_contact o block_user).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Nombre representativo del contacto (para add_contact).",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Destinatario de la regla de veto (nombre, JID, o '*' para todos los no-admin).",
			},
			"rule": map[string]any{
				"type":        "string",
				"description": "Texto descriptivo de la regla de veto/confidencialidad (ej. 'No hablar de proyectos del homelab ni contrasenas').",
			},
			"rule_id": map[string]any{
				"type":        "integer",
				"description": "ID numerico de la regla de veto a eliminar (para delete_veto).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *UsersManagerTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.UserMgr == nil {
		return "El administrador de usuarios no esta inicializado.", nil
	}

	isAdmin, _ := ctx.Value("is_admin").(bool)
	if !isAdmin {
		return "Acceso denegado: solo el administrador puede gestionar usuarios y reglas de confidencialidad.", nil
	}

	var input struct {
		Action  string `json:"action"`
		JID     string `json:"jid"`
		Name    string `json:"name"`
		Target  string `json:"target"`
		Rule    string `json:"rule"`
		RuleID  int64  `json:"rule_id"`
	}

	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	switch input.Action {
	case "list_users":
		list, err := t.UserMgr.ListUsers()
		if err != nil {
			return fmt.Sprintf("Error listando usuarios: %v", err), nil
		}
		if len(list) == 0 {
			return "No hay usuarios registrados en el sistema.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Usuarios y Contactos Registrados (%d):\n", len(list)))
		for _, u := range list {
			sb.WriteString(fmt.Sprintf("- [%s] %s (%s) - Creado: %s\n",
				strings.ToUpper(string(u.Role)), u.Name, u.JID, u.CreatedAt.Format("2006-01-02 15:04")))
		}
		return strings.TrimSpace(sb.String()), nil

	case "add_contact":
		if input.JID == "" {
			return "Debe especificar el campo 'jid' o numero de telefono.", nil
		}
		name := input.Name
		if name == "" {
			name = input.JID
		}
		if err := t.UserMgr.AddOrUpdateUser(input.JID, name, users.RoleContact); err != nil {
			return fmt.Sprintf("Error agregando contacto: %v", err), nil
		}
		return fmt.Sprintf("Contacto '%s' (%s) registrado exitosamente con rol CONTACT.", name, input.JID), nil

	case "block_user":
		if input.JID == "" {
			return "Debe especificar el campo 'jid'.", nil
		}
		if err := t.UserMgr.AddOrUpdateUser(input.JID, input.Name, users.RoleBlocked); err != nil {
			return fmt.Sprintf("Error bloqueando usuario: %v", err), nil
		}
		return fmt.Sprintf("Usuario %s bloqueado exitosamente.", input.JID), nil

	case "list_veto":
		rules, err := t.UserMgr.ListVetoRules()
		if err != nil {
			return fmt.Sprintf("Error consultando reglas de confidencialidad: %v", err), nil
		}
		if len(rules) == 0 {
			return "No hay reglas de confidencialidad o veto activas.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Directivas de Confidencialidad y Veto Activas (%d):\n", len(rules)))
		for _, r := range rules {
			sb.WriteString(fmt.Sprintf("- [#%d] Para '%s': %s (Creada: %s)\n",
				r.ID, r.TargetContact, r.Rule, r.CreatedAt.Format("2006-01-02 15:04")))
		}
		return strings.TrimSpace(sb.String()), nil

	case "add_veto":
		target := input.Target
		if target == "" {
			target = "*"
		}
		if input.Rule == "" {
			return "Debe especificar el texto de la regla en 'rule'.", nil
		}
		if err := t.UserMgr.AddVetoRule(target, input.Rule); err != nil {
			return fmt.Sprintf("Error registrando regla de veto: %v", err), nil
		}
		return fmt.Sprintf("Directiva de confidencialidad para '%s' registrada exitosamente: \"%s\"", target, input.Rule), nil

	case "delete_veto":
		if input.RuleID <= 0 {
			return "Debe especificar un 'rule_id' valido.", nil
		}
		if err := t.UserMgr.DeleteVetoRule(input.RuleID); err != nil {
			return fmt.Sprintf("Error eliminando regla de veto: %v", err), nil
		}
		return fmt.Sprintf("Directiva de confidencialidad #%d eliminada exitosamente.", input.RuleID), nil

	default:
		return fmt.Sprintf("Accion no reconocida '%s'. Use: list_users, add_contact, block_user, list_veto, add_veto, delete_veto.", input.Action), nil
	}
}
