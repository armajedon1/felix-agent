package fastpath

import (
	"strings"
	"testing"
	"time"

	"felix-go/internal/digest"
)

func TestFastPath_Route(t *testing.T) {
	watcher := digest.NewWatcher(10 * time.Minute)
	router := NewRouter(watcher)

	// 1. Consulta de hora
	handled, reply := router.Route("que hora es?", true)
	if !handled || !strings.Contains(reply, "Son las") {
		t.Errorf("Se esperaba respuesta de hora en fastpath, obtenido: %v | %s", handled, reply)
	}

	// 2. Consulta de fecha
	handled, reply = router.Route("que dia es hoy?", true)
	if !handled || !strings.Contains(reply, "Hoy es") {
		t.Errorf("Se esperaba respuesta de fecha en fastpath, obtenido: %v | %s", handled, reply)
	}

	// 3. Ping
	handled, reply = router.Route("ping", true)
	if !handled || !strings.Contains(reply, "Pong") {
		t.Errorf("Se esperaba respuesta de ping en fastpath, obtenido: %v | %s", handled, reply)
	}

	// 4. Saludo
	handled, reply = router.Route("hola", true)
	if !handled || !strings.Contains(reply, "Hola") {
		t.Errorf("Se esperaba respuesta de saludo en fastpath, obtenido: %v | %s", handled, reply)
	}

	// 5. Uptime e IP
	handled, reply = router.Route("uptime", true)
	if !handled || !strings.Contains(reply, "Tiempo activo") {
		t.Errorf("Se esperaba respuesta de uptime en fastpath, obtenido: %v | %s", handled, reply)
	}

	handled, reply = router.Route("tailscale ip", true)
	if !handled || !strings.Contains(reply, "DIRECCIONES IP") {
		t.Errorf("Se esperaba respuesta de IP en fastpath, obtenido: %v | %s", handled, reply)
	}

	// 6. Consulta compleja (debe dar false para que pase al LLM)
	handled, _ = router.Route("necesito que compiles un script de bash y lo ejecutes", true)
	if handled {
		t.Errorf("No se esperaba que fastpath capturara una consulta compleja de ejecucion")
	}
}
