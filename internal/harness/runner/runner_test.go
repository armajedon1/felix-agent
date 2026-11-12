package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDetectProject_Go(t *testing.T) {
	pType, cmd := DetectProject("../../..")
	if pType != ProjectGo {
		t.Errorf("se esperaba ProjectGo, obtenido: %s", pType)
	}
	if !strings.Contains(cmd, "go test") {
		t.Errorf("comando inesperado: %s", cmd)
	}
}

func TestExecute_Success(t *testing.T) {
	res, err := Execute(context.Background(), ".", "echo test_ok", 5*time.Second)
	if err != nil {
		t.Fatalf("error en Execute: %v", err)
	}
	if !res.Success || res.ExitCode != 0 {
		t.Errorf("se esperaba exito, obtenido: %+v", res)
	}
	if !strings.Contains(res.Stdout, "test_ok") {
		t.Errorf("stdout inesperado: %s", res.Stdout)
	}
}

func TestSanitizeError(t *testing.T) {
	rawStderr := `
# github.com/example/pkg
internal/service/auth.go:42:15: undefined: UserToken
internal/service/auth.go:43:8: cannot use token (variable of type string) as type int in argument to Validate
some verbose toolchain notice that should not be prioritized
`
	sanitized := SanitizeError(rawStderr, "")

	if !strings.Contains(sanitized, "auth.go:42:15: undefined: UserToken") {
		t.Errorf("linea critica 1 no capturada: %s", sanitized)
	}
	if !strings.Contains(sanitized, "auth.go:43:8: cannot use token") {
		t.Errorf("linea critica 2 no capturada: %s", sanitized)
	}
}
