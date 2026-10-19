package panelai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

func runACPFixture() {
	scanner := bufio.NewScanner(os.Stdin)
	var mcpURL, authorization string
	sid := "ses_fixture"
	send := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	for scanner.Scan() {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		result := any(map[string]any{})
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}}
		case "session/new", "session/load":
			var params struct {
				MCPServers []struct {
					URL     string `json:"url"`
					Headers []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"headers"`
				} `json:"mcpServers"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if len(params.MCPServers) != 1 {
				os.Exit(3)
			}
			mcpURL = params.MCPServers[0].URL
			for _, header := range params.MCPServers[0].Headers {
				if header.Name == "Authorization" {
					authorization = header.Value
				}
			}
			result = map[string]string{"sessionId": sid}
		case "session/prompt":
			if strings.Contains(string(request.Params), "BLOCK_UNTIL_CANCELLED") {
				_, _ = io.Copy(io.Discard, os.Stdin)
				return
			}
			var settings struct {
				Permission map[string]string `json:"permission"`
				Tools      map[string]bool   `json:"tools"`
			}
			_ = json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &settings)
			if settings.Permission["*"] != "deny" || settings.Tools["*"] || !settings.Tools["panel_audit_query"] {
				os.Exit(4)
			}
			req, _ := http.NewRequest(http.MethodPost, mcpURL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query","arguments":{"mode":"metadata"}}}`))
			req.Header.Set("Authorization", authorization)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				os.Exit(5)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "fixture") {
				os.Exit(6)
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "one", "title": "panel_audit_query", "status": "completed"}}})
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Synthetic audit report"}}}})
			result = map[string]string{"stopReason": "end_turn"}
		}
		send(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}
