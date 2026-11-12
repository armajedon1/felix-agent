package prompts

import (
	_ "embed"
	"strings"
)

//go:embed system_engineer.txt
var systemEngineerPrompt string

//go:embed diff_rules.txt
var diffRulesPrompt string

//go:embed error_correction_template.txt
var errorCorrectionTemplate string

// GetSystemEngineerPrompt retorna la directiva de sistema para ingenieria.
func GetSystemEngineerPrompt() string {
	return systemEngineerPrompt
}

// GetDiffRules retorna las reglas de especificacion de bloques Search/Replace.
func GetDiffRules() string {
	return diffRulesPrompt
}

// FormatErrorCorrection formatea la plantilla de retroalimentacion de errores del compilador.
func FormatErrorCorrection(cmd, sanitizedError, contextCode string) string {
	res := errorCorrectionTemplate
	res = strings.ReplaceAll(res, "{{COMMAND}}", cmd)
	res = strings.ReplaceAll(res, "{{ERROR}}", sanitizedError)
	res = strings.ReplaceAll(res, "{{CONTEXT}}", contextCode)
	return res
}
