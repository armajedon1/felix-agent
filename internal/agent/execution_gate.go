package agent

import (
	"regexp"
	"strings"
)

var affirmativePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*(ok|okay|okey|dale|hacelo|mandale|metele|ejecuta|ejecutalo|procede|adelante|si|confirmo|confirmado|avanza|claro|por supuesto|hace todo|hacelo de una vez|dame los resultados|avanzar|iniciar)\b`),
	regexp.MustCompile(`(?i)\b(hacelo|ejecuta todo|hace todo|mandale|procede|adelante|confirmado|hacelo de una vez|dame los resultados de una vez)\b`),
}

var executionCommandVerbs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*(ejecuta|proba|crea|hace|compila|instala|corre|testea|actualiza|inicializa|inicia|borra|elimina|reinicia)\b`),
}

var hesitationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)¿\s*(procedo|avanzo|lo hago|queres que|desea que|confirmo|ejecuto|hago|disparo|arranco|inicio)\b`),
	regexp.MustCompile(`(?i)\b(confirmame si|confirmame para|avisame si|avisame para|aguardo tu confirmacion|aguardo confirmacion|espero tu confirmacion|si estas de acuerdo|cuando me des el visto bueno|cuando me confirmes|indicame si procedo|decime si avanzo|decime si procedo)\b`),
	regexp.MustCompile(`(?i)se requiere ejecutar\s*:?\s*` + "```"),
}

func normalizeLower(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "á", "a")
	s = strings.ReplaceAll(s, "é", "e")
	s = strings.ReplaceAll(s, "í", "i")
	s = strings.ReplaceAll(s, "ó", "o")
	s = strings.ReplaceAll(s, "ú", "u")
	return s
}

// IsAffirmativeOrExecutionCommand determina si el mensaje del usuario constituye
// una confirmacion afirmativa o una orden imperativa de ejecucion.
func IsAffirmativeOrExecutionCommand(userInput string) bool {
	norm := normalizeLower(strings.TrimSpace(userInput))
	if norm == "" {
		return false
	}

	for _, p := range affirmativePatterns {
		if p.MatchString(norm) {
			return true
		}
	}

	for _, p := range executionCommandVerbs {
		if p.MatchString(norm) {
			return true
		}
	}

	return false
}

// IsHesitatingOrAskingConfirmation detecta si el modelo, en lugar de actuar,
// devolvio texto evasivo solicitando confirmaciones redundantes o delegando pasivamente.
func IsHesitatingOrAskingConfirmation(reply string) bool {
	norm := normalizeLower(strings.TrimSpace(reply))
	if norm == "" {
		return false
	}

	for _, p := range hesitationPatterns {
		if p.MatchString(norm) {
			return true
		}
	}

	return false
}
