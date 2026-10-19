package panelai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type (
	acpEnvelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	acpClient struct {
		command *exec.Cmd
		input   io.WriteCloser
		scanner *bufio.Scanner
		tail    *tailBuffer
		next    int
		update  func(json.RawMessage)
	}
)

func (o *openCode) startACP(ctx context.Context, scheduled bool) (*acpClient, error) {
	configuration, err := loadProviderConfig(o.directory)
	if err != nil {
		return nil, err
	}
	providers, _ := configuration["provider"].(map[string]any)
	if len(providers) == 0 {
		return nil, fmt.Errorf("configure an OpenCode provider and model in AI settings before starting a conversation")
	}
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cmd := o.command(ctx, "acp", "--pure", "--cwd", o.directory)
	permission := map[string]string{"*": "deny", "panel_audit_query": "allow"}
	tools := map[string]bool{"*": false, "panel_audit_query": true, "panel_audit_create_task": !scheduled}
	if !scheduled {
		permission["panel_audit_create_task"] = "allow"
	}
	override := map[string]any{"enabled_providers": ids, "permission": permission, "tools": tools, "default_agent": "build", "agent": map[string]any{"build": map[string]any{"permission": permission, "tools": tools}}, "mcp": map[string]any{}}
	config, _ := json.Marshal(override)
	cmd.Env = append(cmd.Env, "OPENCODE_CONFIG_CONTENT="+string(config))
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	tail := &tailBuffer{}
	cmd.Stderr = tail
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("start OpenCode ACP: %w", err)
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	return &acpClient{command: cmd, input: input, scanner: scanner, tail: tail}, nil
}

func (a *acpClient) close() {
	_ = a.input.Close()
	if a.command.Process != nil {
		_ = killProcess(a.command)
	}
	_ = a.command.Wait()
}
func (a *acpClient) write(value any) error { return json.NewEncoder(a.input).Encode(value) }
func (a *acpClient) call(method string, params, result any) error {
	a.next++
	id := a.next
	if err := a.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("write ACP %s: %w", method, err)
	}
	for a.scanner.Scan() {
		var event acpEnvelope
		if err := json.Unmarshal(a.scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode OpenCode ACP %s: %w", method, err)
		}
		if event.Method != "" {
			if len(event.ID) > 0 {
				if event.Method == "session/request_permission" {
					if err := a.write(map[string]any{"jsonrpc": "2.0", "id": event.ID, "result": map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}}); err != nil {
						return err
					}
				} else {
					if err := a.write(map[string]any{"jsonrpc": "2.0", "id": event.ID, "error": map[string]any{"code": -32601, "message": "Panel does not expose filesystem, terminal or external client tools"}}); err != nil {
						return err
					}
				}
			} else if event.Method == "session/update" && a.update != nil {
				a.update(event.Params)
			}
			continue
		}
		var responseID int
		if json.Unmarshal(event.ID, &responseID) != nil || responseID != id {
			continue
		}
		if event.Error != nil {
			detail := event.Error.Message
			if len(event.Error.Data) > 0 && string(event.Error.Data) != "null" {
				detail += "; " + boundedText(string(event.Error.Data), 2048)
			}
			return fmt.Errorf("OpenCode ACP %s error %d: %s", method, event.Error.Code, detail)
		}
		if result != nil {
			if err := json.Unmarshal(event.Result, result); err != nil {
				return fmt.Errorf("decode ACP %s result: %w", method, err)
			}
		}
		return nil
	}
	if err := a.scanner.Err(); err != nil {
		return fmt.Errorf("read OpenCode ACP %s: %w", method, err)
	}
	return fmt.Errorf("OpenCode ACP ended during %s: %s", method, a.tail.String())
}

func (m *Manager) prompt(ctx context.Context, client *openCode, run *model.AIRun) error {
	acp, err := client.startACP(ctx, run.TaskID != "")
	if err != nil {
		return err
	}
	defer acp.close()
	var initialized struct {
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if err = acp.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]string{"name": "3x-ui", "version": "1"}}, &initialized); err != nil {
		return fmt.Errorf("initialize CLI: %s", redactConfigText(client.directory, err.Error()))
	}
	params := map[string]any{"cwd": client.directory, "mcpServers": []any{map[string]any{"type": "http", "name": "panel_audit", "url": m.mcpURL, "headers": []map[string]string{{"name": "Authorization", "value": "Bearer " + m.token}}}}}
	method := "session/new"
	if run.SessionID != "" {
		if !initialized.AgentCapabilities.LoadSession {
			return fmt.Errorf("OpenCode CLI does not support loading saved sessions; upgrade the CLI")
		}
		method = "session/load"
		params["sessionId"] = run.SessionID
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err = acp.call(method, params, &session); err != nil {
		return fmt.Errorf("open CLI session: %s", redactConfigText(client.directory, strings.ReplaceAll(err.Error(), m.token, "[REDACTED]")))
	}
	if run.SessionID == "" {
		if !strings.HasPrefix(session.SessionID, "ses") {
			return fmt.Errorf("OpenCode ACP returned an invalid session ID")
		}
		run.SessionID = session.SessionID
	}
	if run.ConversationID != "" {
		if err = database.GetDB().Model(&model.AIConversation{}).Where("id = ?", run.ConversationID).Update("session_id", run.SessionID).Error; err != nil {
			return fmt.Errorf("save OpenCode session reference: %w", err)
		}
	}
	if err = database.GetDB().Model(&model.AIRun{}).Where("id = ?", run.ID).Update("session_id", run.SessionID).Error; err != nil {
		return err
	}
	if run.Model != "" {
		if err = acp.call("session/set_model", map[string]any{"sessionId": run.SessionID, "modelId": run.Model}, nil); err != nil {
			return fmt.Errorf("select OpenCode model: %s", redactConfigText(client.directory, err.Error()))
		}
	}
	stream := m.streamFor(run.ID)
	trace := map[string]map[string]string{}
	order := []string{}
	thought := ""
	acp.update = func(raw json.RawMessage) {
		var notice struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				Type    string `json:"sessionUpdate"`
				Content struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				ToolCallID string `json:"toolCallId"`
				Title      string `json:"title"`
				Status     string `json:"status"`
			} `json:"update"`
		}
		if json.Unmarshal(raw, &notice) != nil || notice.SessionID != run.SessionID {
			return
		}
		update := notice.Update
		switch update.Type {
		case "agent_message_chunk":
			if update.Content.Type == "text" {
				run.Result = boundedText(run.Result+update.Content.Text, 128<<10)
			}
		case "agent_thought_chunk":
			if update.Content.Type == "text" {
				thought = boundedText(thought+update.Content.Text, 64<<10)
			}
		case "tool_call", "tool_call_update":
			if len(trace) >= 200 && trace[update.ToolCallID] == nil {
				return
			}
			item := trace[update.ToolCallID]
			if item == nil {
				item = map[string]string{"tool": update.Title, "status": "pending"}
				trace[update.ToolCallID] = item
				order = append(order, update.ToolCallID)
			}
			if update.Title != "" {
				item["tool"] = boundedText(update.Title, 512)
			}
			if update.Status != "" {
				item["status"] = update.Status
			}
			items := []map[string]string{}
			for _, id := range order {
				items = append(items, trace[id])
			}
			data, _ := json.Marshal(items)
			run.Tools = string(data)
		default:
			return
		}
		stream.publish(AIStream{Run: *run, Thought: thought})
	}
	guidance := auditGuidance
	if run.TaskID != "" {
		guidance += "\nThis is a scheduled task execution. Do not create, modify or schedule any tasks."
	}
	var result struct {
		StopReason string `json:"stopReason"`
	}
	if err = acp.call("session/prompt", map[string]any{"sessionId": run.SessionID, "prompt": []map[string]string{{"type": "text", "text": guidance + "\n\nAdministrator request:\n" + run.Prompt}}}, &result); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("OpenCode CLI prompt: %s", redactConfigText(client.directory, strings.ReplaceAll(err.Error(), m.token, "[REDACTED]")))
	}
	if result.StopReason != "end_turn" {
		return fmt.Errorf("OpenCode CLI stopped with reason %q", result.StopReason)
	}
	if strings.TrimSpace(run.Result) == "" {
		return fmt.Errorf("OpenCode completed without a text result; inspect provider/model settings")
	}
	return nil
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}
