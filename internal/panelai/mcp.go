package panelai

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

var taskCreationMu sync.Mutex

type Hooks struct {
	SQL         func(context.Context, string) (any, error)
	Environment func(context.Context) (string, error)
	Notify      func(context.Context, string) error
}

func (m *Manager) environment(ctx context.Context) (string, error) {
	if m.hooks.Environment == nil {
		return "", nil
	}
	return m.hooks.Environment(ctx)
}

func (m *Manager) serveMCP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+m.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	if len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	rpcError := func(code int, message string) { response["error"] = map[string]any{"code": code, "message": message} }
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(request.Params, &params)
		version := "2025-03-26"
		if params.ProtocolVersion == "2024-11-05" {
			version = params.ProtocolVersion
		}
		environment, err := m.environment(r.Context())
		if err != nil {
			rpcError(-32603, err.Error())
			break
		}
		response["result"] = map[string]any{"instructions": auditGuidance + environment, "protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "3x-ui-audit", "version": "2.0.0"}}
	case "ping":
		response["result"] = map[string]any{}
	case "tools/list":
		response["result"] = map[string]any{"tools": auditTools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			rpcError(-32602, "invalid tool arguments")
			break
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, err := m.callTool(ctx, params.Name, params.Arguments)
		text := ""
		if err != nil {
			text = err.Error()
		} else {
			data, encodeErr := json.Marshal(result)
			if encodeErr != nil {
				text = encodeErr.Error()
				err = encodeErr
			} else {
				text = string(data)
			}
		}
		response["result"] = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": err != nil}
	default:
		rpcError(-32601, "method not found")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}

func auditTools() []map[string]any {
	return []map[string]any{
		{"name": "execute_sql", "description": "Execute one read-only SELECT/WITH SQL analysis over the audit log tables described in the environment instructions. Use server-side aggregates, joins, CTEs, HAVING and window functions to analyze all clients efficiently. Filter time ranges, select only necessary columns and rank candidates. Never SELECT * or page through the whole log library. Returns at most 200 rows/64 columns/256 KiB with a truncated flag; refine aggregation when truncated. Uses the panel database dialect.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"sql": map[string]any{"type": "string", "minLength": 1, "maxLength": 32768}}, "required": []string{"sql"}, "additionalProperties": false}},
		{"name": "create_task", "description": "Create a recurring AI task only when the administrator explicitly requests one in the conversation. Store the user's natural-language goal; choose the requested interval and Telegram option. Do not create duplicate tasks or create tasks while executing a scheduled task.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"name": map[string]any{"type": "string", "maxLength": 200}, "prompt": map[string]any{"type": "string", "maxLength": 16000}, "intervalHours": map[string]any{"type": "integer", "minimum": 1, "maximum": 8760}, "telegram": map[string]any{"type": "boolean"},
		}, "required": []string{"name", "prompt", "intervalHours", "telegram"}, "additionalProperties": false}},
	}
}

func (m *Manager) callTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	cfg, err := ReadSettings()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("AI is disabled")
	}
	switch name {
	case "execute_sql":
		var request struct {
			SQL string `json:"sql"`
		}
		decoder := json.NewDecoder(bytes.NewReader(args))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&request); err != nil {
			return nil, fmt.Errorf("decode audit SQL: %w", err)
		}
		if len(request.SQL) == 0 || len(request.SQL) > 32<<10 {
			return nil, fmt.Errorf("audit SQL must contain 1..32768 bytes")
		}
		if m.hooks.SQL == nil {
			return nil, fmt.Errorf("audit SQL tool is unavailable")
		}
		return m.hooks.SQL(ctx, request.SQL)
	case "create_task":
		var request struct {
			Name          string `json:"name"`
			Prompt        string `json:"prompt"`
			IntervalHours int    `json:"intervalHours"`
			Telegram      bool   `json:"telegram"`
		}
		decoder := json.NewDecoder(bytes.NewReader(args))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&request); err != nil {
			return nil, fmt.Errorf("decode task creation arguments: %w", err)
		}
		task := AITaskInput{Name: request.Name, Prompt: request.Prompt, IntervalHours: request.IntervalHours, Telegram: request.Telegram}

		taskCreationMu.Lock()
		defer taskCreationMu.Unlock()
		var existing model.AITask
		lookup := database.GetDB().Where("name = ? AND prompt = ? AND interval_hours = ? AND telegram = ? AND enabled = ?", strings.TrimSpace(task.Name), strings.TrimSpace(task.Prompt), task.IntervalHours, task.Telegram, true).First(&existing).Error
		if lookup == nil {
			return &existing, nil
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("check duplicate AI task: %w", lookup)
		}
		task.Enabled = true
		return SaveTask(task)
	default:
		return nil, fmt.Errorf("unknown audit tool %q", name)
	}
}
