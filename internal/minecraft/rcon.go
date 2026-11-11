package minecraft

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultAddress  = "127.0.0.1:25575"
	DefaultPassword = "McPanelSecret2026!"
	DefaultTimeout  = 3 * time.Second

	// Tipos de paquetes del protocolo Source RCON
	PacketResponseValue = 0 // Servidor -> Cliente (salida de comando)
	PacketExecCommand   = 2 // Cliente -> Servidor (ejecutar comando)
	PacketAuthResponse  = 2 // Servidor -> Cliente (respuesta a autenticacion)
	PacketAuth          = 3 // Cliente -> Servidor (solicitud de autenticacion)
)

var mcColorRegex = regexp.MustCompile("§[0-9a-fk-orA-FK-OR]")

// CleanMinecraftText remueve codigos de formato y color de Minecraft (simbolo de seccion).
func CleanMinecraftText(text string) string {
	cleaned := mcColorRegex.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}

// Client implementa un cliente Source RCON nativo en Go puro.
type Client struct {
	addr     string
	password string
	timeout  time.Duration
}

// NewClient crea una nueva instancia del cliente RCON con los parametros especificados.
func NewClient(addr, password string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if addr == "" {
		addr = DefaultAddress
	}
	if password == "" {
		password = DefaultPassword
	}
	return &Client{
		addr:     addr,
		password: password,
		timeout:  timeout,
	}
}

// DefaultClient retorna un cliente configurado con los parametros predeterminados del homelab.
func DefaultClient() *Client {
	return NewClient(DefaultAddress, DefaultPassword, DefaultTimeout)
}

// writePacket escribe un paquete Source RCON en el writer.
func writePacket(w io.Writer, id, packetType int32, body string) error {
	bodyBytes := []byte(body)
	// Tamano del cuerpo + 4 bytes ID + 4 bytes Type + 2 bytes nulos finales
	size := int32(4 + 4 + len(bodyBytes) + 2)
	buf := make([]byte, 4+size)

	binary.LittleEndian.PutUint32(buf[0:4], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(id))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(packetType))
	copy(buf[12:12+len(bodyBytes)], bodyBytes)
	buf[12+len(bodyBytes)] = 0x00
	buf[13+len(bodyBytes)] = 0x00

	_, err := w.Write(buf)
	return err
}

// readPacket lee y decodifica un paquete Source RCON desde el reader.
func readPacket(r io.Reader) (id, packetType int32, body string, err error) {
	var sizeBytes [4]byte
	if _, err := io.ReadFull(r, sizeBytes[:]); err != nil {
		return 0, 0, "", err
	}

	size := int32(binary.LittleEndian.Uint32(sizeBytes[:]))
	if size < 10 || size > 65536 {
		return 0, 0, "", fmt.Errorf("tamano de paquete rcon invalido: %d", size)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, 0, "", err
	}

	id = int32(binary.LittleEndian.Uint32(data[0:4]))
	packetType = int32(binary.LittleEndian.Uint32(data[4:8]))

	bodyLen := size - 10
	if bodyLen > 0 {
		bodyBytes := data[8 : 8+bodyLen]
		bodyBytes = bytes.TrimRight(bodyBytes, "\x00")
		body = string(bodyBytes)
	}

	return id, packetType, body, nil
}

// Execute conecta al servidor Minecraft via RCON, autentica, ejecuta el comando y retorna la salida limpia.
func (c *Client) Execute(cmd string) (string, error) {
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return "", fmt.Errorf("error conectando a servidor minecraft RCON (%s): %w", c.addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(c.timeout))

	// 1. Autenticacion
	authReqID := int32(42)
	if err := writePacket(conn, authReqID, PacketAuth, c.password); err != nil {
		return "", fmt.Errorf("error enviando autenticacion RCON: %w", err)
	}

	authenticated := false
	for {
		respID, respType, _, err := readPacket(conn)
		if err != nil {
			return "", fmt.Errorf("error leyendo respuesta de autenticacion RCON: %w", err)
		}

		if respID == -1 {
			return "", errors.New("autenticacion RCON rechazada: clave incorrecta")
		}

		if respType == PacketAuthResponse {
			if respID != authReqID {
				return "", fmt.Errorf("ID de autenticacion inesperado: %d (esperado %d)", respID, authReqID)
			}
			authenticated = true
			break
		}
		// Si recibimos un paquete previo con tipo PacketResponseValue (algunas versiones envian un paquete vacio previo),
		// continuamos el ciclo para leer el PacketAuthResponse subsiguiente.
	}

	if !authenticated {
		return "", errors.New("no se recibio confirmacion de autenticacion RCON")
	}

	// 2. Ejecutar comando
	cmdReqID := int32(100)
	if err := writePacket(conn, cmdReqID, PacketExecCommand, cmd); err != nil {
		return "", fmt.Errorf("error enviando comando RCON: %w", err)
	}

	_, _, respBody, err := readPacket(conn)
	if err != nil {
		return "", fmt.Errorf("error leyendo respuesta de comando RCON: %w", err)
	}

	return CleanMinecraftText(respBody), nil
}

// ListPlayers consulta los jugadores conectados en el servidor.
func (c *Client) ListPlayers() (string, error) {
	return c.Execute("list")
}

// WhitelistList consulta la lista de jugadores habilitados en la whitelist.
func (c *Client) WhitelistList() (string, error) {
	return c.Execute("whitelist list")
}

// WhitelistAdd agrega un jugador a la whitelist.
func (c *Client) WhitelistAdd(user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", errors.New("nombre de usuario no especificado")
	}
	return c.Execute("whitelist add " + user)
}

// WhitelistRemove remueve un jugador de la whitelist.
func (c *Client) WhitelistRemove(user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", errors.New("nombre de usuario no especificado")
	}
	return c.Execute("whitelist remove " + user)
}

// WhitelistReload recarga la configuracion de la whitelist en el servidor.
func (c *Client) WhitelistReload() (string, error) {
	return c.Execute("whitelist reload")
}

// Say difunde un mensaje en el chat general de Minecraft.
func (c *Client) Say(msg string) (string, error) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", errors.New("mensaje no especificado")
	}
	return c.Execute("say " + msg)
}

// Kick expulsa a un jugador del servidor.
func (c *Client) Kick(user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", errors.New("nombre de usuario no especificado")
	}
	return c.Execute("kick " + user)
}
