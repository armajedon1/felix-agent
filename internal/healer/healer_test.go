package healer

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExtractPortFromLog(t *testing.T) {
	testCases := []struct {
		logText      string
		expectedPort int
	}{
		{
			logText:      "Error: listen EADDRINUSE: address already in use :::3000\n    at Server.setupListenHandle",
			expectedPort: 3000,
		},
		{
			logText:      "listen EADDRINUSE: address already in use 0.0.0.0:8080",
			expectedPort: 8080,
		},
		{
			logText:      "bind error: :5070 address already in use",
			expectedPort: 5070,
		},
		{
			logText:      "SocketException: port 25575 is already in use",
			expectedPort: 25575,
		},
		{
			logText:      "TypeError: Cannot read properties of undefined\n    at main.js:42",
			expectedPort: 0,
		},
		{
			logText:      "",
			expectedPort: 0,
		},
	}

	for _, tc := range testCases {
		port := ExtractPortFromLog(tc.logText)
		if port != tc.expectedPort {
			t.Errorf("ExtractPortFromLog(%q) = %d; esperado %d", tc.logText, port, tc.expectedPort)
		}
	}
}

func TestHealReport_FormatMarkdown(t *testing.T) {
	rep := &HealReport{
		Timestamp: time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC),
		Checked:   3,
		Healthy:   2,
		Unhealthy: 1,
		Recovered: 1,
		Failed:    0,
		Services: []ServiceHealStatus{
			{
				Name:    "web-service",
				Type:    "pm2",
				Status:  "online",
				Healthy: true,
				PID:     1234,
			},
			{
				Name:    "vaultwarden",
				Type:    "docker",
				Status:  "running",
				Healthy: true,
			},
			{
				Name:        "api-service",
				Type:        "pm2",
				Status:      "errored",
				Healthy:     false,
				Cause:       "Puerto 3200 en conflicto (EADDRINUSE)",
				ActionTaken: "Puerto 3200 liberado con fuser -k y proceso reiniciado (pm2 restart mision-sitio)",
				NewStatus:   "online",
			},
		},
	}

	md := rep.FormatMarkdown()

	expectedSnippets := []string{
		"INFORME DE AUTO-CURACION DE INFRAESTRUCTURA",
		"Servicios analizados: 3",
		"Operativos: 2",
		"Incidentes: 1",
		"Recuperaciones exitosas: 1",
		"web-service",
		"vaultwarden",
		"api-service",
		"RECUPERADO",
		"Puerto 3200 en conflicto",
	}

	for _, s := range expectedSnippets {
		if !strings.Contains(md, s) {
			t.Errorf("FormatMarkdown deberia contener %q. Resultado:\n%s", s, md)
		}
	}
}

func TestEngine_Check(t *testing.T) {
	eng := NewEngine()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rep, err := eng.Check(ctx, "servicio-inexistente-para-test")
	if err != nil {
		t.Fatalf("Check fallo inesperadamente: %v", err)
	}

	if rep.Checked != 1 {
		t.Errorf("Se esperaba 1 servicio chequeado, obtenidos %d", rep.Checked)
	}

	if rep.Unhealthy != 1 {
		t.Errorf("Se esperaba 1 servicio unhealthy para app inexistente, obtenidos %d", rep.Unhealthy)
	}
}
