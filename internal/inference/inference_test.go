package inference

import (
	"testing"
)

func TestPresets(t *testing.T) {
	gemini, ok := GetPreset("gemini")
	if !ok || gemini.BaseURL == "" {
		t.Fatalf("error obteniendo preset de Gemini")
	}

	openai, ok := GetPreset("openai")
	if !ok || openai.DefaultModel != "gpt-4o" {
		t.Fatalf("error en preset de OpenAI")
	}

	groq, ok := GetPreset("groq")
	if !ok || groq.DefaultModel != "llama-3.3-70b-versatile" {
		t.Fatalf("error en preset de Groq")
	}

	ollama, ok := GetPreset("ollama")
	if !ok || ollama.RequiresKey {
		t.Fatalf("error en preset de Ollama")
	}
}

func TestKeyPool(t *testing.T) {
	kp := NewKeyPool("key-1", "key-2")
	if kp.Count() != 2 {
		t.Errorf("se esperaban 2 claves, se obtuvieron %d", kp.Count())
	}

	k1 := kp.GetCurrentKey()
	if k1 != "key-1" {
		t.Errorf("clave inicial inesperada: %s", k1)
	}

	k2 := kp.Rotate()
	if k2 != "key-2" {
		t.Errorf("clave rotada inesperada: %s", k2)
	}

	k3 := kp.Rotate()
	if k3 != "key-1" {
		t.Errorf("rotacion circular fallida: %s", k3)
	}

	masked := kp.GetAllMasked()
	if len(masked) != 2 {
		t.Errorf("se esperaban 2 claves enmascaradas")
	}
}
