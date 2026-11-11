package minecraft

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// startMockRCONServer levanta un servidor TCP mock local para simular el protocolo RCON.
func startMockRCONServer(t *testing.T, expectedPass string, responses map[string]string) (string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("error creando listener mock: %v", err)
	}

	stopChan := make(chan struct{})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-stopChan:
					return
				default:
					return
				}
			}

			go handleMockConnection(t, conn, expectedPass, responses)
		}
	}()

	cleanup := func() {
		close(stopChan)
		_ = listener.Close()
	}

	return listener.Addr().String(), cleanup
}

func handleMockConnection(t *testing.T, conn net.Conn, expectedPass string, responses map[string]string) {
	defer conn.Close()

	for {
		id, pType, body, err := readPacket(conn)
		if err != nil {
			return
		}

		switch pType {
		case PacketAuth:
			if body == expectedPass {
				// Autenticacion exitosa
				if err := writePacket(conn, id, PacketAuthResponse, ""); err != nil {
					return
				}
			} else {
				// Autenticacion fallida
				if err := writePacket(conn, -1, PacketAuthResponse, ""); err != nil {
					return
				}
				return
			}
		case PacketExecCommand:
			respText, ok := responses[body]
			if !ok {
				respText = "Comando desconocido en mock: " + body
			}
			if err := writePacket(conn, id, PacketResponseValue, respText); err != nil {
				return
			}
		}
	}
}

func TestRCON_SuccessfulAuthAndCommand(t *testing.T) {
	responses := map[string]string{
		"list": "§aThere are 2 of a max of 20 players online:§r player1, player2",
	}

	addr, cleanup := startMockRCONServer(t, "SecretTestPass", responses)
	defer cleanup()

	client := NewClient(addr, "SecretTestPass", 1*time.Second)
	out, err := client.Execute("list")
	if err != nil {
		t.Fatalf("Execute fallo: %v", err)
	}

	expected := "There are 2 of a max of 20 players online: player1, player2"
	if out != expected {
		t.Errorf("Salida inesperada. Esperado: %q, Obtenido: %q", expected, out)
	}
}

func TestRCON_AuthFailure(t *testing.T) {
	addr, cleanup := startMockRCONServer(t, "CorrectPassword", nil)
	defer cleanup()

	client := NewClient(addr, "WrongPassword", 1*time.Second)
	_, err := client.Execute("list")
	if err == nil {
		t.Fatal("Se esperaba error por clave incorrecta, pero no ocurrio ninguno")
	}

	if !strings.Contains(err.Error(), "autenticacion RCON rechazada") {
		t.Errorf("Mensaje de error inesperado: %v", err)
	}
}

func TestRCON_HelperMethods(t *testing.T) {
	responses := map[string]string{
		"list":                    "There are 0 of a max of 20 players online:",
		"whitelist list":          "There are 2 whitelisted player(s): armajedon1, Amahura",
		"whitelist add Alex":      "Added Alex to the whitelist",
		"whitelist remove Alex":   "Removed Alex from the whitelist",
		"whitelist reload":        "Reloaded the whitelist",
		"say Servidor en marcha":  "[Server] Servidor en marcha",
		"kick Steve":              "Kicked Steve from the game",
	}

	addr, cleanup := startMockRCONServer(t, "Secret123", responses)
	defer cleanup()

	client := NewClient(addr, "Secret123", 1*time.Second)

	// ListPlayers
	out, err := client.ListPlayers()
	if err != nil || !strings.Contains(out, "players online") {
		t.Errorf("ListPlayers fallo: out=%q, err=%v", out, err)
	}

	// WhitelistList
	out, err = client.WhitelistList()
	if err != nil || !strings.Contains(out, "whitelisted player(s)") {
		t.Errorf("WhitelistList fallo: out=%q, err=%v", out, err)
	}

	// WhitelistAdd
	out, err = client.WhitelistAdd("Alex")
	if err != nil || !strings.Contains(out, "Added Alex") {
		t.Errorf("WhitelistAdd fallo: out=%q, err=%v", out, err)
	}

	// WhitelistRemove
	out, err = client.WhitelistRemove("Alex")
	if err != nil || !strings.Contains(out, "Removed Alex") {
		t.Errorf("WhitelistRemove fallo: out=%q, err=%v", out, err)
	}

	// WhitelistReload
	out, err = client.WhitelistReload()
	if err != nil || !strings.Contains(out, "Reloaded") {
		t.Errorf("WhitelistReload fallo: out=%q, err=%v", out, err)
	}

	// Say
	out, err = client.Say("Servidor en marcha")
	if err != nil || !strings.Contains(out, "Servidor en marcha") {
		t.Errorf("Say fallo: out=%q, err=%v", out, err)
	}

	// Kick
	out, err = client.Kick("Steve")
	if err != nil || !strings.Contains(out, "Kicked Steve") {
		t.Errorf("Kick fallo: out=%q, err=%v", out, err)
	}
}

func TestRCON_ValidationErrors(t *testing.T) {
	client := DefaultClient()

	if _, err := client.WhitelistAdd(""); err == nil {
		t.Error("WhitelistAdd sin usuario deberia fallar")
	}
	if _, err := client.WhitelistRemove(""); err == nil {
		t.Error("WhitelistRemove sin usuario deberia fallar")
	}
	if _, err := client.Say(""); err == nil {
		t.Error("Say sin mensaje deberia fallar")
	}
	if _, err := client.Kick(""); err == nil {
		t.Error("Kick sin usuario deberia fallar")
	}
}

func TestRCON_TimeoutAndUnreachable(t *testing.T) {
	// Puerto cerrado en localhost
	client := NewClient("127.0.0.1:44999", "password", 100*time.Millisecond)
	_, err := client.Execute("list")
	if err == nil {
		t.Fatal("Se esperaba error al conectar a puerto cerrado")
	}
}

func TestRCON_CleanMinecraftText(t *testing.T) {
	raw := "§aOnline: §r§6[Admin]§r §bNotch§r, §eJeb§r"
	cleaned := CleanMinecraftText(raw)
	expected := "Online: [Admin] Notch, Jeb"
	if cleaned != expected {
		t.Errorf("CleanMinecraftText fallo. Esperado: %q, Obtenido: %q", expected, cleaned)
	}
}

func TestRCON_PacketSerialization(t *testing.T) {
	var buf bytes.Buffer
	err := writePacket(&buf, 1234, PacketAuth, "TestPass")
	if err != nil {
		t.Fatalf("writePacket fallo: %v", err)
	}

	id, pType, body, err := readPacket(&buf)
	if err != nil {
		t.Fatalf("readPacket fallo: %v", err)
	}

	if id != 1234 || pType != PacketAuth || body != "TestPass" {
		t.Errorf("Datos deserializados no coinciden: id=%d, type=%d, body=%q", id, pType, body)
	}
}

func TestRCON_PacketSizeLimit(t *testing.T) {
	// Paquete con tamano invalido (< 10)
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, int32(5))
	buf.WriteString("12345")

	_, _, _, err := readPacket(&buf)
	if err == nil {
		t.Fatal("readPacket deberia rechazar tamano < 10")
	}

	// Paquete con EOF prematuro
	buf.Reset()
	binary.Write(&buf, binary.LittleEndian, int32(20))
	buf.WriteString("short")

	_, _, _, err = readPacket(&buf)
	if err != io.ErrUnexpectedEOF {
		t.Errorf("Esperado io.ErrUnexpectedEOF, obtenido: %v", err)
	}
}
