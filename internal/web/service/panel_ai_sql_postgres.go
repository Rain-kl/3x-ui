package service

import (
	"encoding/json"
	"fmt"
	"strings"

	pgquery "github.com/pganalyze/pg_query_go/v6"
	"gorm.io/gorm"
)

func validatePostgresAuditSQL(db *gorm.DB, query string) (string, error) {
	parsed, err := pgquery.Parse(query)
	if err != nil {
		return "", fmt.Errorf("parse audit DQL: %w", err)
	}
	if len(parsed.Stmts) != 1 || parsed.Stmts[0].Stmt.GetSelectStmt() == nil {
		return "", fmt.Errorf("audit SQL accepts a single SELECT or WITH query")
	}
	raw, err := pgquery.ParseToJSON(query)
	if err != nil {
		return "", fmt.Errorf("inspect audit DQL: %w", err)
	}
	var tree any
	if err = json.Unmarshal([]byte(raw), &tree); err != nil {
		return "", fmt.Errorf("decode audit DQL syntax: %w", err)
	}
	ctes := map[string]bool{}
	functions := map[string]bool{}
	operators := map[string]bool{}
	types := map[string]bool{}
	err = walkSQLTree(tree, func(key string, node map[string]any) error {
		if key == "CommonTableExpr" {
			name, _ := node["ctename"].(string)
			ctes[name] = true
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	err = walkSQLTree(tree, func(key string, node map[string]any) error {
		if strings.HasSuffix(key, "Stmt") && key != "SelectStmt" {
			return fmt.Errorf("audit SQL forbids %s", key)
		}
		switch key {
		case "SelectStmt":
			if node["intoClause"] != nil || node["lockingClause"] != nil {
				return fmt.Errorf("audit SQL forbids INTO and row locking")
			}
		case "RangeVar":
			table, _ := node["relname"].(string)
			schema, _ := node["schemaname"].(string)
			catalog, _ := node["catalogname"].(string)
			localCTE := schema == "" && ctes[table]
			logTable := (schema == "" || schema == "public") && aiLogTables[table]
			if catalog != "" || (!logTable && !localCTE) {
				return fmt.Errorf("audit SQL table %q.%q is outside the log schema", schema, table)
			}
		case "FuncCall":
			name, err := builtinSQLName(node["funcname"])
			if err != nil {
				return err
			}
			for _, prefix := range []string{"query_to_", "table_to_", "database_to_", "schema_to_", "cursor_to_", "pg_read_", "pg_ls_", "dblink"} {
				if strings.HasPrefix(name, prefix) {
					return fmt.Errorf("audit SQL forbids indirect SQL/file access function %q", name)
				}
			}
			if name == "ts_stat" {
				return fmt.Errorf("audit SQL forbids indirect SQL function %q", name)
			}
			functions[name] = true
		case "TypeName":
			name, err := builtinSQLName(node["names"])
			if err != nil {
				return err
			}
			types[name] = true
		case "A_Expr":
			if kind, _ := node["kind"].(string); strings.Contains(kind, "BETWEEN") {
				break
			}
			name, err := builtinSQLName(node["name"])
			if err != nil {
				return err
			}
			operators[name] = true
		case "RangeTableSample":
			name, err := builtinSQLName(node["method"])
			if err != nil {
				return err
			}
			if name != "system" && name != "bernoulli" {
				return fmt.Errorf("audit SQL forbids custom table sample %q", name)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(functions) > 0 {
		names := make([]string, 0, len(functions))
		for name := range functions {
			names = append(names, name)
		}
		var available []string
		err = db.Raw("SELECT DISTINCT proname FROM pg_catalog.pg_proc WHERE pronamespace = 'pg_catalog'::regnamespace AND provolatile IN ('i','s') AND proname IN ?", names).Scan(&available).Error
		if err != nil {
			return "", fmt.Errorf("validate audit SQL functions: %w", err)
		}
		for _, name := range available {
			delete(functions, name)
		}
		if len(functions) > 0 {
			return "", fmt.Errorf("audit SQL requires built-in non-volatile functions; rejected %v", functions)
		}
	}
	if len(types) > 0 {
		names := make([]string, 0, len(types))
		for name := range types {
			names = append(names, name)
		}
		var available []string
		err = db.Raw("SELECT typname FROM pg_catalog.pg_type WHERE typnamespace = 'pg_catalog'::regnamespace AND typname IN ?", names).Scan(&available).Error
		if err != nil {
			return "", fmt.Errorf("validate audit SQL types: %w", err)
		}
		for _, name := range available {
			delete(types, name)
		}
		if len(types) > 0 {
			return "", fmt.Errorf("audit SQL requires built-in types; rejected %v", types)
		}
	}
	if len(operators) > 0 {
		names := make([]string, 0, len(operators))
		for name := range operators {
			names = append(names, name)
		}
		var available []string
		err = db.Raw("SELECT DISTINCT o.oprname FROM pg_catalog.pg_operator o JOIN pg_catalog.pg_proc p ON p.oid=o.oprcode WHERE o.oprnamespace='pg_catalog'::regnamespace AND p.provolatile IN ('i','s') AND o.oprname IN ?", names).Scan(&available).Error
		if err != nil {
			return "", fmt.Errorf("validate audit SQL operators: %w", err)
		}
		for _, name := range available {
			delete(operators, name)
		}
		if len(operators) > 0 {
			return "", fmt.Errorf("audit SQL requires built-in non-volatile operators; rejected %v", operators)
		}
	}
	canonical, err := pgquery.Deparse(parsed)
	if err != nil {
		return "", fmt.Errorf("normalize audit DQL: %w", err)
	}
	var explanation string
	if err = db.Raw("EXPLAIN (FORMAT JSON, VERBOSE, COSTS FALSE) " + canonical).Scan(&explanation).Error; err != nil {
		return "", fmt.Errorf("plan audit DQL: %w", err)
	}
	var plan any
	if err = json.Unmarshal([]byte(explanation), &plan); err != nil {
		return "", fmt.Errorf("decode audit DQL plan: %w", err)
	}
	if err = walkSQLTree(plan, func(_ string, node map[string]any) error {
		if relation, ok := node["Relation Name"].(string); ok {
			schema, _ := node["Schema"].(string)
			if !aiLogTables[relation] || schema != "public" {
				return fmt.Errorf("audit DQL plan reads non-log relation %q.%q", schema, relation)
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	return canonical, nil
}

func builtinSQLName(value any) (string, error) {
	parts, ok := value.([]any)
	if !ok || len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("audit SQL requires built-in names")
	}
	names := []string{}
	for _, part := range parts {
		item, _ := part.(map[string]any)
		str, _ := item["String"].(map[string]any)
		name, _ := str["sval"].(string)
		names = append(names, name)
	}
	if names[len(names)-1] == "" || (len(names) == 2 && names[0] != "pg_catalog") {
		return "", fmt.Errorf("audit SQL forbids custom namespace %v", names)
	}
	return names[len(names)-1], nil
}

func walkSQLTree(value any, visit func(string, map[string]any) error) error {
	switch current := value.(type) {
	case map[string]any:
		for key, value := range current {
			if node, ok := value.(map[string]any); ok {
				if err := visit(key, node); err != nil {
					return err
				}
			}
			if err := walkSQLTree(value, visit); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range current {
			if err := walkSQLTree(value, visit); err != nil {
				return err
			}
		}
	}
	return nil
}
