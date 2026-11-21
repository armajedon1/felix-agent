package main

import (
	"fmt"
	"os"
	"runtime"
)

func PrintMOTD() {
	fmt.Println("================================================================================")
	fmt.Println("          ███████╗███████╗██╗     ██╗██╗  ██╗")
	fmt.Println("          ██╔════╝██╔════╝██║     ██║╚██╗██╔╝")
	fmt.Println("          █████╗  █████╗  ██║     ██║ ╚███╔╝ ")
	fmt.Println("          ██╔══╝  ██╔══╝  ██║     ██║ ██╔██╗ ")
	fmt.Println("          ██║     ███████╗███████╗██║██╔╝ ██╗")
	fmt.Println("          ╚═╝     ╚══════╝╚══════╝╚═╝╚═╝  ╚═╝")
	fmt.Println("          A G E N T  //  S W E  &  D E V O P S")
	fmt.Println("================================================================================")
	fmt.Println(" Modo: Consola de Administracion y Control (Felix)")
	fmt.Printf(" Plataforma: Linux (%s/%s) | Runtime: Go Nativo\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println(" Soberania: Republica Argentina [ARG] | Licencia: MIT Open Source")
	fmt.Println(" Repositorio: https://github.com/m-leiva/felix-agent")
	fmt.Println("================================================================================")
}

func PrintHelp() {
	PrintMOTD()
	fmt.Println("Uso: felix <comando> [argumentos...]")
	fmt.Println()
	fmt.Println("Comandos Principales:")
	fmt.Println("  chat [mensaje]     Inicia sesion de chat interactivo o ejecuta un prompt directo")
	fmt.Println("  setup / onboard    Asistente interactivo de configuracion y proveedores LLM")
	fmt.Println("  login / auth       Gestion de inicio de sesion OAuth y estado de Antigravity Tools")
	fmt.Println("  status / doctor    Diagnostico de conexion de inferencia, latencia y servicios")
	fmt.Println("  daemon / server    Inicia el agente en modo demonio permanente 24/7")
	fmt.Println("  help               Muestra esta ayuda")
	fmt.Println()
	fmt.Println("Opciones de Inferencia:")
	fmt.Println("  --config <path>    Ruta al archivo config.json (por defecto config.json)")
	fmt.Println("  --cli <prompt>     Ejecuta un prompt directo por terminal y finaliza")
	fmt.Println("  --setup            Inicia directamente el asistente de configuracion")
	fmt.Println("================================================================================")
}

func isTerminal() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
