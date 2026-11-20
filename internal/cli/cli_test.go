package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsCLICommand(t *testing.T) {
	valid := []string{"cli", "status", "logs", "conv", "conversations", "sessions", "config", "bots", "bot", "chat", "version", "-v", "--version", "help", "-h", "--help"}
	for _, cmd := range valid {
		if !IsCLICommand(cmd) {
			t.Errorf("se esperaba que '%s' fuera reconocido como comando CLI", cmd)
		}
	}

	invalid := []string{"foo", "bar", "--invalid", "start", "daemon"}
	for _, cmd := range invalid {
		if IsCLICommand(cmd) {
			t.Errorf("no se esperaba que '%s' fuera reconocido como comando CLI", cmd)
		}
	}
}

func TestPrintBanner(t *testing.T) {
	// Verificar que PrintBanner y PrintHelp no causen panico
	PrintBanner()
	PrintVersion()
	PrintHelp()
}

func TestCmdConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfgJSON := `{
  "model": "gemini-2.5-flash",
  "api_base": "http://127.0.0.1:8045/v1",
  "api_key": "sk-1234567890",
  "data_dir": "` + tmpDir + `"
}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("error escribiendo config temporal: %v", err)
	}

	// 1. show
	if code := CmdConfig([]string{"show"}, cfgPath); code != 0 {
		t.Errorf("CmdConfig show retorno codigo %d", code)
	}

	// 2. get
	if code := CmdConfig([]string{"get", "model"}, cfgPath); code != 0 {
		t.Errorf("CmdConfig get retorno codigo %d", code)
	}

	// 3. set
	if code := CmdConfig([]string{"set", "model", "qwen/qwen3.8-27b"}, cfgPath); code != 0 {
		t.Errorf("CmdConfig set retorno codigo %d", code)
	}

	// Verificar persistencia
	updated, err := os.ReadFile(cfgPath)
	if err != nil || !filepath.HasPrefix(string(updated), "{") {
		t.Errorf("error leyendo config actualizado")
	}
}

func TestCmdBots(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgJSON := `{"data_dir": "` + tmpDir + `"}`
	_ = os.WriteFile(cfgPath, []byte(cfgJSON), 0644)

	// 1. list (vacio)
	if code := CmdBots([]string{"list"}, cfgPath); code != 0 {
		t.Errorf("CmdBots list retorno %d", code)
	}

	// 2. create pizzeria
	if code := CmdBots([]string{"create", "--template", "pizzeria", "--id", "piz-test", "--name", "Pizzeria Test"}, cfgPath); code != 0 {
		t.Errorf("CmdBots create retorno %d", code)
	}

	// 3. show
	if code := CmdBots([]string{"show", "piz-test"}, cfgPath); code != 0 {
		t.Errorf("CmdBots show retorno %d", code)
	}

	// 4. replicate
	if code := CmdBots([]string{"replicate", "piz-test", "piz-test-2", "--name", "Pizzeria Test 2"}, cfgPath); code != 0 {
		t.Errorf("CmdBots replicate retorno %d", code)
	}

	// 5. delete
	if code := CmdBots([]string{"delete", "piz-test-2"}, cfgPath); code != 0 {
		t.Errorf("CmdBots delete retorno %d", code)
	}
}
