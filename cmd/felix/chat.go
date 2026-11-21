package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"felix-go/internal/agent"
	"felix-go/internal/scheduler"
)

func runInteractiveChat(engine *agent.Engine, contextMem string, sched *scheduler.Scheduler) {
	PrintMOTD()
	fmt.Println("Consola de Chat Directo con Felix Core")
	fmt.Println("Escribe tu consulta o tarea de DevOps/programacion (o ':exit' para salir)")
	fmt.Println("Comandos especiales:")
	fmt.Println("  :help   Muestra comandos de la consola")
	fmt.Println("  :clear  Limpia la pantalla")
	fmt.Println("  :exit   Finaliza la sesion interactiva")
	fmt.Println("--------------------------------------------------------------------------------")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("felix> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == ":exit" || line == "salir" || line == "exit" || line == "quit" {
			fmt.Println("Sesion de chat finalizada.")
			break
		}
		if line == ":clear" || line == "clear" || line == "cls" {
			fmt.Print("\033[H\033[2J")
			PrintMOTD()
			continue
		}
		if line == ":help" {
			fmt.Println("Comandos disponibles:")
			fmt.Println("  :help   Muestra esta lista de comandos")
			fmt.Println("  :clear  Limpia la pantalla")
			fmt.Println("  :exit   Sale del chat")
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		reply, err := engine.Run(ctx, "cli-session", line, contextMem)
		cancel()

		if err != nil {
			fmt.Printf("\n[ERROR] %v\n\n", err)
			continue
		}

		if sched != nil {
			reply = sched.ParseAndSchedule(reply, "cli-chat", "Admin")
		}

		fmt.Println()
		fmt.Println(reply)
		fmt.Println("--------------------------------------------------------------------------------")
	}
}
