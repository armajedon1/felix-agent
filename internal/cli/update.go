package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"felix-go/internal/config"
)

// CmdUpdate busca actualizaciones en GitHub, descarga cambios y recompila el binario.
func CmdUpdate(args []string, configPath string) int {
	autoYes := false
	for _, a := range args {
		if a == "-y" || a == "--yes" {
			autoYes = true
		}
	}

	PrintBanner()
	fmt.Println("================================================================================")
	fmt.Println("             SISTEMA DE ACTUALIZACION AUTOMATICA DE FELIX                       ")
	fmt.Println("================================================================================")

	// 1. Localizar el directorio del repositorio
	repoDir := config.FindRepoDir()
	if repoDir == "" {
		fmt.Println("[ERROR] No se pudo encontrar el directorio del repositorio de Felix.")
		fmt.Println("Asegurate de ejecutar el comando dentro del repositorio clonado, o clona el repo:")
		fmt.Println("  git clone https://github.com/m-leiva/felix-agent.git ~/felix-agent")
		return 1
	}

	fmt.Printf("Directorio del repositorio: %s\n", repoDir)
	fmt.Println("Comprobando actualizaciones en GitHub (origin/main)...")

	// 2. git fetch
	fetchCmd := exec.Command("git", "fetch", "origin", "main")
	fetchCmd.Dir = repoDir
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		fmt.Printf("[ERROR] No se pudo conectar con GitHub: %v\n%s\n", err, string(out))
		return 1
	}

	// 3. Comparar HEAD con origin/main
	localRevCmd := exec.Command("git", "rev-parse", "HEAD")
	localRevCmd.Dir = repoDir
	localOut, _ := localRevCmd.Output()
	localCommit := strings.TrimSpace(string(localOut))

	remoteRevCmd := exec.Command("git", "rev-parse", "origin/main")
	remoteRevCmd.Dir = repoDir
	remoteOut, _ := remoteRevCmd.Output()
	remoteCommit := strings.TrimSpace(string(remoteOut))

	if localCommit == remoteCommit {
		short := localCommit
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Printf("[OK] Felix ya esta en la version mas reciente (%s).\n", short)
		return 0
	}

	// 4. Mostrar commits entrantes
	logCmd := exec.Command("git", "log", "HEAD..origin/main", "--oneline")
	logCmd.Dir = repoDir
	logOut, _ := logCmd.Output()

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("Nuevas actualizaciones disponibles:")
	fmt.Println(string(logOut))
	fmt.Println("--------------------------------------------------------------------------------")

	if !autoYes {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Deseas aplicar la actualizacion y recompilar ahora? [S/n]: ")
		ans, _ := reader.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans != "" && ans != "s" && ans != "si" && ans != "y" && ans != "yes" {
			fmt.Println("Actualizacion cancelada.")
			return 0
		}
	}

	// 5. git pull
	fmt.Println("Descargando cambios desde GitHub...")
	pullCmd := exec.Command("git", "pull", "origin", "main")
	pullCmd.Dir = repoDir
	if out, err := pullCmd.CombinedOutput(); err != nil {
		fmt.Printf("[ERROR] Error al descargar cambios: %v\n%s\n", err, string(out))
		return 1
	}

	// 6. Recompilar
	fmt.Println("Compilando nueva version del binario nativo...")
	binDir := filepath.Join(repoDir, "bin")
	_ = os.MkdirAll(binDir, 0755)
	binPath := filepath.Join(binDir, "felix")
	buildCmd := exec.Command("go", "build", "-o", binPath, "cmd/felix/main.go")
	buildCmd.Dir = repoDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Printf("[ERROR] Error compilando Felix: %v\n%s\n", err, string(out))
		return 1
	}

	// 7. Instalar binario
	serverBinDir := filepath.Join(os.Getenv("HOME"), "felix-server", "bin")
	if _, err := os.Stat(serverBinDir); err == nil {
		serverBin := filepath.Join(serverBinDir, "felix")
		_ = exec.Command("install", "-m", "755", binPath, serverBin).Run()
	}

	targetDir := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	_ = os.MkdirAll(targetDir, 0755)
	targetBin := filepath.Join(targetDir, "felix")

	installCmd := exec.Command("install", "-m", "755", binPath, targetBin)
	if out, err := installCmd.CombinedOutput(); err != nil {
		fmt.Printf("[AVISO] No se pudo instalar en %s: %v\n%s\n", targetBin, err, string(out))
	} else {
		fmt.Printf("[OK] Binario instalado exitosamente en %s\n", targetBin)
	}

	// Limpiar lanzadores de escritorio obsoletos
	_ = os.Remove(filepath.Join(os.Getenv("HOME"), ".local", "share", "applications", "felix.desktop"))

	// 8. Reiniciar PM2 si aplica
	if _, err := exec.LookPath("pm2"); err == nil {
		checkPM2 := exec.Command("pm2", "describe", "felix-server")
		if err := checkPM2.Run(); err == nil {
			fmt.Println("Reiniciando servicio en segundo plano felix-server (PM2)...")
			_ = exec.Command("pm2", "restart", "felix-server").Run()
		}
	}

	newRevCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	newRevCmd.Dir = repoDir
	newOut, _ := newRevCmd.Output()

	fmt.Println("================================================================================")
	fmt.Printf("[OK] Felix actualizado exitosamente a la version %s!\n", strings.TrimSpace(string(newOut)))
	fmt.Println("================================================================================")
	return 0
}
