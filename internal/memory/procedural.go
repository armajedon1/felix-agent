package memory

import (
	"fmt"
	"strings"
)

// ProceduralSkill representa un procedimiento operativo estandar (SOP) probado y libre de fallos.
type ProceduralSkill struct {
	Title     string
	Keywords  []string
	Procedure string
}

var standardProcedures = []ProceduralSkill{
	{
		Title:    "Gestion de Contenedores Docker",
		Keywords: []string{"docker", "contenedor", "compose", "container", "docker ps"},
		Procedure: `- Utilizar la herramienta docker_action o comandos bash nativos:
  * Listar contenedores: docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
  * Ver logs: docker logs --tail 100 <contenedor>
  * Reiniciar: docker restart <contenedor>`,
	},
	{
		Title:    "Administracion de Servicios en Linux (Systemd y PM2)",
		Keywords: []string{"servicio", "systemctl", "daemon", "pm2", "proceso"},
		Procedure: `- Inspeccionar y controlar servicios con herramientas de sistema:
  * PM2: pm2 status / pm2 logs <app> --lines 50 / pm2 reload <app>
  * Systemd: systemctl status <servicio> / systemctl restart <servicio>`,
	},
	{
		Title:    "Inspeccion y Consultas de Bases de Datos SQLite",
		Keywords: []string{"sqlite", "base de datos", "tabla", "query", "sql"},
		Procedure: `- Utilizar sqlite_query o sqlite_inspector:
  * Ver esquema: SELECT sql FROM sqlite_master WHERE type='table';
  * Contar registros: SELECT count(*) FROM <tabla>;
  * Muestreo: SELECT * FROM <tabla> ORDER BY rowid DESC LIMIT 5;`,
	},
	{
		Title:    "Operaciones Git y Control de Versiones",
		Keywords: []string{"git", "commit", "diff", "branch", "repositorio"},
		Procedure: `- Utilizar la herramienta git_operations:
  * Estado: git status -s
  * Diferencias: git diff
  * Historial: git log --oneline -n 5
  * Regla estricta: mensajes de commit sobrios, concisos y sin emojis.`,
	},
	{
		Title:    "Respaldos y Mantenimiento de Almacenamiento",
		Keywords: []string{"backup", "respaldo", "disco", "almacenamiento", "limpieza"},
		Procedure: `- Gestionar respaldos atomicos y espacio en disco:
  * Respaldo atomico SQLite: VACUUM INTO 'data/backups/db_<timestamp>.db'
  * Espacio libre: df -h /
  * Limpieza: purgar logs antiguos o temporales en data/downloads/`,
	},
}

// GetMatchingProcedure busca si la consulta del usuario coincide con algun procedimiento operativo estandar.
func GetMatchingProcedure(query string) string {
	qLower := strings.ToLower(query)

	for _, p := range standardProcedures {
		for _, kw := range p.Keywords {
			if strings.Contains(qLower, kw) {
				return fmt.Sprintf("### PROCEDIMIENTO ESTANDAR RECOMENDADO (%s):\n%s\n", p.Title, p.Procedure)
			}
		}
	}
	return ""
}
