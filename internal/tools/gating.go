package tools

import (
	"strings"
)

// ToolCategory agrupa herramientas por dominio funcional.
type ToolCategory string

const (
	CatCore      ToolCategory = "core"
	CatInfra     ToolCategory = "infra"
	CatCodeFiles ToolCategory = "code_files"
	CatWeb       ToolCategory = "web"
	CatMedia     ToolCategory = "media"
	CatAdmin     ToolCategory = "admin"
)

var toolCategoryMap = map[string]ToolCategory{
	// Core (Memoria e Historial)
	"memory_search":   CatCore,
	"session_search":  CatCore,
	"session_history": CatCore,
	"memory_manager":  CatCore,

	// Infraestructura y Homelab
	"exec":              CatInfra,
	"process":           CatInfra,
	"infra":             CatInfra,
	"infra_healer":      CatInfra,
	"docker":            CatInfra,
	"homelab":           CatInfra,
	"net_sentinel":      CatInfra,
	"minecraft":         CatInfra,
	"minecraft_control": CatInfra,
	"workstation":       CatInfra,
	"workstation_link":  CatInfra,
	"pc_task":           CatInfra,
	"tailscale_tool":    CatInfra,

	// Archivos, Codigo y Skills
	"file_read":       CatCodeFiles,
	"file_write":      CatCodeFiles,
	"file_edit":       CatCodeFiles,
	"file_ls":         CatCodeFiles,
	"apply_patch":     CatCodeFiles,
	"antigravity_cli": CatCodeFiles,
	"git_tool":        CatCodeFiles,
	"skills_list":     CatCodeFiles,
	"skills_read":     CatCodeFiles,
	"skills_write":    CatCodeFiles,
	"doc_engine":      CatCodeFiles,

	// Navegacion Web y Clima
	"web_search":    CatWeb,
	"web_fetch":     CatWeb,
	"browser":       CatWeb,
	"browser_agent": CatWeb,
	"weather":       CatWeb,

	// Multimedia y Mensajeria
	"send_message":   CatMedia,
	"send_file":      CatMedia,
	"send_voice":     CatMedia,
	"vision_inspect": CatMedia,
	"image_generate": CatMedia,

	// Administracion y Tareas
	"users_manager":     CatAdmin,
	"goal_manager":      CatAdmin,
	"reminders":         CatAdmin,
	"subagent_spawn":    CatAdmin,
	"background_worker": CatAdmin,
	"sqlite_query":      CatAdmin,
	"sqlite_inspector":  CatAdmin,
	"gcli":              CatAdmin,
	"scratchpad":        CatAdmin,
}

var categoryKeywords = map[ToolCategory][]string{
	CatInfra: {
		"server", "servidor", "homelab", "docker", "pm2", "reinicio", "restart", "reiniciar",
		"log", "logs", "puerto", "puertos", "red", "ip", "tailscale", "minecraft", "mc",
		"workstation", "remoto", "pc-task", "pc_task", "comando", "bash", "terminal", "exec",
		"memoria", "temperatura", "disco", "ram", "cpu", "systemctl", "servicio",
		"compilar", "build", "go build", "make", "instalar", "binario",
	},
	CatCodeFiles: {
		"archivo", "archivos", "file", "files", "leer", "escribir", "editar", "modificar",
		"codigo", "code", "go", "python", "javascript", "script", "compilar", "build",
		"antigravity", "agy", "skill", "skills", "doc", "pdf", "docx", "markdown",
		"arreglar", "cambiar", "programar", "desarrollar", "parche", "patch",
	},
	CatWeb: {
		"buscar", "google", "web", "pagina", "sitio", "url", "link", "noticias",
		"clima", "pronostico", "tiempo", "lluvia", "llover", "llov", "temperatura", "scrape", "navegar", "browser",
	},
	CatMedia: {
		"audio", "voz", "nota de voz", "hablar", "tts", "imagen", "foto", "captura",
		"transcribir", "documento", "enviar archivo", "adjunto",
	},
	CatAdmin: {
		"recordar", "recordatorio", "aviso", "avisar", "alarma", "cron", "usuario", "contacto",
		"rol", "veto", "confidencial", "subagente", "background", "tarea", "sqlite",
		"base de datos", "tabla", "gcli", "drive", "gmail", "calendar", "scratchpad",
	},
}

// SelectTools filtra inteligentemente el catalogo de herramientas.
// Para el administrador, garantiza autonomía total entregando siempre el catálogo completo
// de herramientas operativas (idéntico al comportamiento de OpenClaw, sin que filtros heurísticos
// deshabiliten herramientas críticas como exec o file_edit ante mensajes breves o de seguimiento).
// Para no-administradores, aplica un filtrado estricto de seguridad por categoría y rol.
func (r *Registry) SelectTools(query, activeGoal string, isAdmin bool) []OpenAIToolSchema {
	allSchemas := r.GetOpenAISchemasForRole(isAdmin)
	if len(allSchemas) == 0 {
		return nil
	}

	// El administrador siempre dispone del 100% de las herramientas en todos los turnos.
	if isAdmin {
		return allSchemas
	}

	qLower := strings.ToLower(query + " " + activeGoal)

	selectedCats := make(map[ToolCategory]bool)
	// CatCore siempre disponible para consultas de memoria e historial
	selectedCats[CatCore] = true

	for cat, kwList := range categoryKeywords {
		for _, kw := range kwList {
			if strings.Contains(qLower, kw) {
				selectedCats[cat] = true
				break
			}
		}
	}

	// Si es una consulta compleja que no matchea palabras clave directas pero pide realizar acciones,
	// habilitamos Core + Web por precaucion.
	if len(selectedCats) == 1 && (strings.Contains(qLower, "como") || strings.Contains(qLower, "hacer") || strings.Contains(qLower, "busca") || strings.Contains(qLower, "podes")) {
		selectedCats[CatWeb] = true
	}

	filtered := make([]OpenAIToolSchema, 0, len(allSchemas))
	for _, schema := range allSchemas {
		cat, exists := toolCategoryMap[schema.Function.Name]
		if !exists {
			// Herramienta sin categoria explicita: incluir por seguridad
			filtered = append(filtered, schema)
			continue
		}
		if selectedCats[cat] {
			filtered = append(filtered, schema)
		}
	}

	// Si por alguna razon el filtro queda vacio, retornar todas las permitidas para el rol
	if len(filtered) == 0 {
		return allSchemas
	}

	return filtered
}
