package inference

import (
	"strings"
	"sync"
	"sync/atomic"
)

type ProviderType string

const (
	ProviderGemini      ProviderType = "gemini"
	ProviderOpenAI      ProviderType = "openai"
	ProviderGroq        ProviderType = "groq"
	ProviderOllama      ProviderType = "ollama"
	ProviderAntigravity ProviderType = "antigravity"
	ProviderCustom      ProviderType = "custom"
)

type Preset struct {
	Type         ProviderType `json:"type"`
	Name         string       `json:"name"`
	BaseURL      string       `json:"base_url"`
	DefaultModel string       `json:"default_model"`
	RequiresKey  bool         `json:"requires_key"`
	Description  string       `json:"description"`
}

var Presets = map[ProviderType]Preset{
	ProviderGemini: {
		Type:         ProviderGemini,
		Name:         "Google Gemini",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai/",
		DefaultModel: "gemini-2.5-flash",
		RequiresKey:  true,
		Description:  "Modelos Gemini de Google con compatibilidad OpenAI nativa.",
	},
	ProviderOpenAI: {
		Type:         ProviderOpenAI,
		Name:         "OpenAI Oficial",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-4o",
		RequiresKey:  true,
		Description:  "Modelos GPT-4o y mini de OpenAI.",
	},
	ProviderGroq: {
		Type:         ProviderGroq,
		Name:         "Groq Cloud",
		BaseURL:      "https://api.groq.com/openai/v1",
		DefaultModel: "llama-3.3-70b-versatile",
		RequiresKey:  true,
		Description:  "Inferencia LPU ultra-rapida con Llama 3.3.",
	},
	ProviderOllama: {
		Type:         ProviderOllama,
		Name:         "Ollama Local",
		BaseURL:      "http://localhost:11434/v1",
		DefaultModel: "llama3",
		RequiresKey:  false,
		Description:  "Servidor de inferencia local sin dependencias en la nube.",
	},
	ProviderAntigravity: {
		Type:         ProviderAntigravity,
		Name:         "Antigravity Tools / Local Proxy",
		BaseURL:      "http://127.0.0.1:8045/v1",
		DefaultModel: "gemini-2.5-flash",
		RequiresKey:  false,
		Description:  "Proxy local de rotacion de cuentas y balanceo.",
	},
}

func GetPreset(name string) (Preset, bool) {
	clean := strings.ToLower(strings.TrimSpace(name))
	p, ok := Presets[ProviderType(clean)]
	return p, ok
}

// KeyPool gestiona un conjunto de claves de API con rotacion automatica.
type KeyPool struct {
	mu    sync.RWMutex
	keys  []string
	index uint64
}

func NewKeyPool(initialKeys ...string) *KeyPool {
	kp := &KeyPool{
		keys: make([]string, 0),
	}
	for _, k := range initialKeys {
		kClean := strings.TrimSpace(k)
		if kClean != "" {
			kp.keys = append(kp.keys, kClean)
		}
	}
	return kp
}

func (kp *KeyPool) AddKey(key string) {
	kClean := strings.TrimSpace(key)
	if kClean == "" {
		return
	}
	kp.mu.Lock()
	defer kp.mu.Unlock()
	for _, existing := range kp.keys {
		if existing == kClean {
			return
		}
	}
	kp.keys = append(kp.keys, kClean)
}

func (kp *KeyPool) SetKeys(keys []string) {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	var cleanList []string
	for _, k := range keys {
		kClean := strings.TrimSpace(k)
		if kClean != "" {
			cleanList = append(cleanList, kClean)
		}
	}
	kp.keys = cleanList
	atomic.StoreUint64(&kp.index, 0)
}

func (kp *KeyPool) GetCurrentKey() string {
	kp.mu.RLock()
	defer kp.mu.RUnlock()
	if len(kp.keys) == 0 {
		return ""
	}
	idx := atomic.LoadUint64(&kp.index) % uint64(len(kp.keys))
	return kp.keys[idx]
}

func (kp *KeyPool) Rotate() string {
	kp.mu.RLock()
	defer kp.mu.RUnlock()
	if len(kp.keys) <= 1 {
		if len(kp.keys) == 1 {
			return kp.keys[0]
		}
		return ""
	}
	newIdx := atomic.AddUint64(&kp.index, 1) % uint64(len(kp.keys))
	return kp.keys[newIdx]
}

func (kp *KeyPool) Count() int {
	kp.mu.RLock()
	defer kp.mu.RUnlock()
	return len(kp.keys)
}

func (kp *KeyPool) GetAllMasked() []string {
	kp.mu.RLock()
	defer kp.mu.RUnlock()
	var masked []string
	for _, k := range kp.keys {
		if len(k) > 8 {
			masked = append(masked, k[:4]+"..."+k[len(k)-4:])
		} else {
			masked = append(masked, "***")
		}
	}
	return masked
}
