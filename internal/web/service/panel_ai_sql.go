package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

var aiSQLDriverOnce sync.Once

var aiLogTables = map[string]bool{
	"audit_events": true, "audit_daily_stats": true, "audit_minute_stats": true,
	"audit_bandwidth_minutes": true, "audit_receipts": true, "subscription_accesses": true,
}

type aiSQLResult struct {
	Columns   []string  `json:"columns"`
	Rows      [][]any   `json:"rows"`
	Truncated bool      `json:"truncated"`
	RowLimit  int       `json:"rowLimit"`
	AsOf      time.Time `json:"asOf"`
}

func queryAISQL(parent context.Context, query string) (any, error) {
	if len(query) == 0 || len(query) > 32<<10 {
		return nil, fmt.Errorf("audit SQL must contain 1..32768 bytes")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	result := aiSQLResult{Rows: [][]any{}, RowLimit: 200, AsOf: time.Now().UTC()}
	err := auditReadSnapshotContext(ctx, func(tx *gorm.DB) (err error) {
		if isAuditChild(tx) {
			return fmt.Errorf("audit SQL is centralized; use the master panel")
		}
		if database.IsPostgres() {
			if err = tx.Exec("SET LOCAL statement_timeout = '30s'").Error; err != nil {
				return fmt.Errorf("set audit SQL timeout: %w", err)
			}
			if err = tx.Exec("SET LOCAL search_path = pg_catalog, public").Error; err != nil {
				return fmt.Errorf("set audit SQL namespace: %w", err)
			}
			query, err = validatePostgresAuditSQL(tx, query)
			if err != nil {
				return err
			}
		}
		statement := "SELECT * FROM (\n" + query + "\n) AS panel_analysis LIMIT 201"
		var rows *sql.Rows
		if database.IsPostgres() {
			rows, err = tx.Raw(statement).Rows()
		} else {
			query, err = sqliteAuditStatement(query)
			if err != nil {
				return err
			}
			var sources []struct {
				Name string
				File string
			}
			if err = tx.Raw("PRAGMA database_list").Scan(&sources).Error; err != nil {
				return fmt.Errorf("locate audit SQLite database: %w", err)
			}
			filename := ""
			for _, source := range sources {
				if source.Name == "main" {
					filename = source.File
				}
			}
			if filename == "" {
				return fmt.Errorf("audit SQL requires a file-backed SQLite database")
			}
			aiSQLDriverOnce.Do(func() {
				sql.Register("xui-ai-audit-readonly", &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
					conn.SetLimit(sqlite3.SQLITE_LIMIT_LENGTH, 1<<20)
					conn.SetLimit(sqlite3.SQLITE_LIMIT_EXPR_DEPTH, 128)
					conn.RegisterAuthorizer(authorizeAuditSQL)
					return nil
				}})
			})
			uri := url.URL{Scheme: "file", Path: filename}
			uri.RawQuery = "mode=ro&_query_only=true&_busy_timeout=5000"
			reader, openErr := sql.Open("xui-ai-audit-readonly", uri.String())
			if openErr != nil {
				return fmt.Errorf("open read-only audit database: %w", openErr)
			}
			defer reader.Close()
			reader.SetMaxOpenConns(1)
			rows, err = reader.QueryContext(ctx, "SELECT * FROM (\n"+query+"\n) AS panel_analysis LIMIT 201")
		}
		if err != nil {
			return fmt.Errorf("execute audit DQL: %w", err)
		}
		defer rows.Close()
		result.Columns, err = rows.Columns()
		if err != nil {
			return fmt.Errorf("read audit SQL columns: %w", err)
		}
		if len(result.Columns) > 64 {
			return fmt.Errorf("audit SQL returns %d columns; select at most 64 specific fields", len(result.Columns))
		}
		columnData, encodeErr := json.Marshal(result.Columns)
		if encodeErr != nil {
			return fmt.Errorf("encode audit SQL columns: %w", encodeErr)
		}
		size := len(columnData) + 512
		for rows.Next() {
			if len(result.Rows) == result.RowLimit {
				result.Truncated = true
				break
			}
			values := make([]any, len(result.Columns))
			destinations := make([]any, len(values))
			for i := range values {
				destinations[i] = &values[i]
			}
			if err = rows.Scan(destinations...); err != nil {
				return fmt.Errorf("scan audit SQL result: %w", err)
			}
			for i, value := range values {
				if data, ok := value.([]byte); ok {
					value = string(data)
				}
				if text, ok := value.(string); ok && len(text) > 4096 {
					value = text[:4096] + " [truncated]"
					result.Truncated = true
				}
				values[i] = value
			}
			encoded, encodeErr := json.Marshal(values)
			if encodeErr != nil {
				return fmt.Errorf("encode audit SQL result: %w", encodeErr)
			}
			size += len(encoded)
			if size > 256<<10 {
				result.Truncated = true
				break
			}
			result.Rows = append(result.Rows, values)
		}
		if err = rows.Err(); err != nil {
			return fmt.Errorf("read audit SQL result: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func authorizeAuditSQL(action int, first, second, databaseName string) int {
	switch action {
	case sqlite3.SQLITE_SELECT, 33:
		return sqlite3.SQLITE_OK
	case sqlite3.SQLITE_READ:
		if databaseName == "main" && aiLogTables[first] {
			return sqlite3.SQLITE_OK
		}
	case sqlite3.SQLITE_FUNCTION:
		switch strings.ToLower(second) {
		case "load_extension", "writefile", "readfile", "eval", "fts3_tokenizer":
			return sqlite3.SQLITE_DENY
		default:
			return sqlite3.SQLITE_OK
		}
	}
	return sqlite3.SQLITE_DENY
}

func sqliteAuditStatement(query string) (string, error) {
	first := ""
	end := -1
	for i := 0; i < len(query); {
		ch := query[i]
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
			i++
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			offset := strings.Index(query[i+2:], "*/")
			if offset < 0 {
				return "", fmt.Errorf("unterminated audit SQL comment")
			}
			i += offset + 4
			continue
		}
		if end >= 0 {
			return "", fmt.Errorf("audit SQL accepts one DQL statement")
		}
		if first == "" {
			start := i
			for i < len(query) && ((query[i] >= 'a' && query[i] <= 'z') || (query[i] >= 'A' && query[i] <= 'Z')) {
				i++
			}
			first = strings.ToUpper(query[start:i])
			if first != "SELECT" && first != "WITH" {
				return "", fmt.Errorf("audit SQL accepts SELECT or WITH only")
			}
			continue
		}
		if ch == ';' {
			end = i
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' || ch == '[' {
			closeQuote := ch
			if ch == '[' {
				closeQuote = ']'
			}
			i++
			closed := false
			for i < len(query) {
				if query[i] == closeQuote {
					if closeQuote != ']' && i+1 < len(query) && query[i+1] == closeQuote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", fmt.Errorf("unterminated audit SQL literal or identifier")
			}
			continue
		}
		i++
	}
	if first == "" {
		return "", fmt.Errorf("audit SQL is empty")
	}
	if end >= 0 {
		query = query[:end]
	}
	return query, nil
}
