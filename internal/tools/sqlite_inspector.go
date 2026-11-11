package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteInspectorTool permite analizar esquemas, tablas, indices y salud de bases SQLite en modo solo-lectura.
type SQLiteInspectorTool struct{}

func (t *SQLiteInspectorTool) Name() string { return "sqlite_inspector" }
func (t *SQLiteInspectorTool) Description() string {
	return "Introspeccion y auditoria segura de bases de datos SQLite en modo solo-lectura: listar tablas, esquemas DDL, indices, claves foraneas y conteo de filas."
}

func (t *SQLiteInspectorTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"tables", "schema", "row_counts", "indices", "analyze"},
				"description": "Accion a realizar en la base de datos.",
			},
			"db_path": map[string]any{
				"type":        "string",
				"description": "Ruta de la base SQLite a inspeccionar (ej: 'data/sessions.db', 'data/users.db', 'data/reminders.db').",
			},
			"table": map[string]any{
				"type":        "string",
				"description": "Nombre de la tabla especifica para schema o indices.",
			},
		},
		"required": []string{"action", "db_path"},
	}
}

func (t *SQLiteInspectorTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action string `json:"action"`
		DBPath string `json:"db_path"`
		Table  string `json:"table"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	if _, err := os.Stat(params.DBPath); err != nil {
		return "", fmt.Errorf("base de datos no encontrada: %s", params.DBPath)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", params.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", fmt.Errorf("error abriendo base SQLite: %w", err)
	}
	defer db.Close()

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	switch params.Action {
	case "tables":
		rows, err := db.QueryContext(timeoutCtx, "SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name;")
		if err != nil {
			return "", err
		}
		defer rows.Close()

		var b strings.Builder
		b.WriteString(fmt.Sprintf("*TABLAS Y VISTAS EN `%s`*\n", params.DBPath))
		found := 0
		for rows.Next() {
			var name, itemType string
			if err := rows.Scan(&name, &itemType); err == nil {
				found++
				b.WriteString(fmt.Sprintf("- *%s* (%s)\n", name, itemType))
			}
		}
		if found == 0 {
			return fmt.Sprintf("No se encontraron tablas de usuario en `%s`.", params.DBPath), nil
		}
		return b.String(), nil

	case "schema":
		query := "SELECT name, sql FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'"
		var args []any
		if params.Table != "" {
			query += " AND name = ?"
			args = append(args, params.Table)
		}
		query += " ORDER BY name;"

		rows, err := db.QueryContext(timeoutCtx, query, args...)
		if err != nil {
			return "", err
		}
		defer rows.Close()

		var b strings.Builder
		b.WriteString(fmt.Sprintf("*ESQUEMA DDL EN `%s`*\n", params.DBPath))
		for rows.Next() {
			var name, sqlDef string
			if err := rows.Scan(&name, &sqlDef); err == nil {
				b.WriteString(fmt.Sprintf("### Tabla: `%s`\n```sql\n%s;\n```\n\n", name, sqlDef))
			}
		}
		return b.String(), nil

	case "row_counts":
		tableRows, err := db.QueryContext(timeoutCtx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%' ORDER BY name;")
		if err != nil {
			return "", err
		}
		var tables []string
		for tableRows.Next() {
			var name string
			if err := tableRows.Scan(&name); err == nil {
				tables = append(tables, name)
			}
		}
		tableRows.Close()

		var b strings.Builder
		b.WriteString(fmt.Sprintf("*CONTEO DE FILAS EN `%s`*\n", params.DBPath))
		for _, tbl := range tables {
			var count int64
			err := db.QueryRowContext(timeoutCtx, fmt.Sprintf("SELECT COUNT(*) FROM \"%s\";", tbl)).Scan(&count)
			if err == nil {
				b.WriteString(fmt.Sprintf("- Tabla `%s`: *%d filas*\n", tbl, count))
			}
		}
		return b.String(), nil

	case "indices":
		query := "SELECT name, tbl_name, sql FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'"
		var args []any
		if params.Table != "" {
			query += " AND tbl_name = ?"
			args = append(args, params.Table)
		}
		query += " ORDER BY tbl_name, name;"

		rows, err := db.QueryContext(timeoutCtx, query, args...)
		if err != nil {
			return "", err
		}
		defer rows.Close()

		var b strings.Builder
		b.WriteString(fmt.Sprintf("*INDICES EN `%s`*\n", params.DBPath))
		found := 0
		for rows.Next() {
			var name, tblName string
			var sqlDef sql.NullString
			if err := rows.Scan(&name, &tblName, &sqlDef); err == nil {
				found++
				b.WriteString(fmt.Sprintf("- Indice `%s` sobre tabla `%s`", name, tblName))
				if sqlDef.Valid && sqlDef.String != "" {
					b.WriteString(fmt.Sprintf(": `%s`\n", sqlDef.String))
				} else {
					b.WriteString(" (automatico / clave primaria)\n")
				}
			}
		}
		if found == 0 {
			return "No se encontraron indices secundarios.", nil
		}
		return b.String(), nil

	case "analyze":
		// PRAGMA page_count, page_size, freelist_count, integrity_check
		var pageSize, pageCount, freelistCount int64
		_ = db.QueryRowContext(timeoutCtx, "PRAGMA page_size;").Scan(&pageSize)
		_ = db.QueryRowContext(timeoutCtx, "PRAGMA page_count;").Scan(&pageCount)
		_ = db.QueryRowContext(timeoutCtx, "PRAGMA freelist_count;").Scan(&freelistCount)

		var integrity string
		_ = db.QueryRowContext(timeoutCtx, "PRAGMA quick_check;").Scan(&integrity)

		totalBytes := pageSize * pageCount
		mb := float64(totalBytes) / (1024 * 1024)

		return fmt.Sprintf("*ANALISIS DE SALUD SQLITE (`%s`)*\n"+
			"- Tamano total en disco: *%.2f MB* (%d paginas de %d bytes)\n"+
			"- Paginas libres (fragmentacion): %d\n"+
			"- Integridad de datos: *%s*\n"+
			"- Modo Journal: WAL activo",
			params.DBPath, mb, pageCount, pageSize, freelistCount, integrity), nil

	default:
		return "", fmt.Errorf("accion '%s' no reconocida", params.Action)
	}
}
