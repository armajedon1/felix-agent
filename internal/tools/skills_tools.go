package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillsListTool lista el catalogo completo de skills disponibles en el sistema.
type SkillsListTool struct {
	SkillsDir string
}

func (t *SkillsListTool) Name() string { return "skills_list" }
func (t *SkillsListTool) Description() string {
	return "Lista todas las habilidades y procedimientos especializados (skills) disponibles en el sistema con su nombre y descripcion."
}
func (t *SkillsListTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func getAllSkillDirs(customDir string) []string {
	var dirs []string
	if customDir != "" {
		dirs = append(dirs, customDir)
	}
	candidates := []string{
		"./skills",
		"skills",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".felix", "skills"),
			filepath.Join(home, ".agents", "skills"),
		)
	}
	seen := make(map[string]bool)
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			dirs = append(dirs, c)
		}
	}
	return dirs
}

func (t *SkillsListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	dirs := getAllSkillDirs(t.SkillsDir)
	seenSkills := make(map[string]bool)
	var list []SkillSummary

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if seenSkills[name] {
				continue
			}
			skillPath := filepath.Join(dir, name, "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}
			seenSkills[name] = true
			desc := extractDescription(string(data))
			list = append(list, SkillSummary{
				Name:        name,
				Description: desc,
				Path:        skillPath,
			})
		}
	}

	resBytes, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return "", err
	}
	return string(resBytes), nil
}

// SkillsReadTool lee las instrucciones completas de una skill especializada.
type SkillsReadTool struct {
	SkillsDir string
}

func (t *SkillsReadTool) Name() string { return "skills_read" }
func (t *SkillsReadTool) Description() string {
	return "Lee las instrucciones tecnicas y procedimientos de un archivo SKILL.md dado su nombre."
}
func (t *SkillsReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Nombre de la skill (ej: 'homelab-control', 'antigravity-coding-agent', 'fpdf-markdown-pdf').",
			},
		},
		"required": []string{"name"},
	}
}
func (t *SkillsReadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	dirs := getAllSkillDirs(t.SkillsDir)
	var skillPath string
	for _, dir := range dirs {
		p := filepath.Join(dir, params.Name, "SKILL.md")
		if _, err := os.Stat(p); err == nil {
			skillPath = p
			break
		}
	}
	if skillPath == "" {
		return "", fmt.Errorf("no se encontro la skill '%s' en ninguna de las rutas de skills", params.Name)
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("error leyendo la skill '%s': %w", params.Name, err)
	}

	return string(data), nil
}

// SkillsWriteTool permite al agente crear o actualizar una nueva habilidad o procedimiento aprendido.
type SkillsWriteTool struct {
	SkillsDir string
}

func (t *SkillsWriteTool) Name() string { return "skills_write" }
func (t *SkillsWriteTool) Description() string {
	return "Crea o actualiza una nueva habilidad operativa persistente (skill) en disco para que el agente recuerde procedimientos complejos a futuro."
}
func (t *SkillsWriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Identificador en kebab-case de la nueva habilidad (ej: 'backup-manager', 'nginx-reverse-proxy').",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Breve resumen de una linea sobre que hace y cuando usarla.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "Contenido Markdown detallado con instrucciones paso a paso, comandos y reglas.",
			},
		},
		"required": []string{"name", "description", "instructions"},
	}
}
func (t *SkillsWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Name == "" || params.Instructions == "" {
		return "", errors.New("parametros 'name' e 'instructions' son obligatorios")
	}

	skillsDir := t.SkillsDir
	if skillsDir == "" {
		skillsDir = "./skills"
	}

	targetDir := filepath.Join(skillsDir, params.Name)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("error creando directorio de skill: %w", err)
	}

	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", params.Name, params.Description, strings.TrimSpace(params.Instructions))
	skillPath := filepath.Join(targetDir, "SKILL.md")

	if err := os.WriteFile(skillPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("error guardando SKILL.md: %w", err)
	}

	return fmt.Sprintf("Skill '%s' persistida exitosamente en %s", params.Name, skillPath), nil
}

func extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter && strings.HasPrefix(trimmed, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
		if !inFrontmatter && strings.HasPrefix(trimmed, "# ") {
			continue
		}
		if !inFrontmatter && trimmed != "" && !strings.HasPrefix(trimmed, "```") {
			if len(trimmed) > 120 {
				return trimmed[:120] + "..."
			}
			return trimmed
		}
	}
	return "Sin descripcion"
}
