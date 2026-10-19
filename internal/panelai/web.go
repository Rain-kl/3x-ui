package panelai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type webProcess struct {
	cancel   context.CancelFunc
	input    io.WriteCloser
	done     chan struct{}
	url      *url.URL
	password string
}

func (m *Manager) stopWeb() {
	m.webMu.Lock()
	defer m.webMu.Unlock()
	if m.web == nil {
		return
	}
	m.web.cancel()
	_ = m.web.input.Close()
	<-m.web.done
	m.web = nil
}

func (m *Manager) WebEndpoint(ctx context.Context) (*url.URL, string, error) {
	m.webMu.Lock()
	defer m.webMu.Unlock()
	if m.web != nil {
		select {
		case <-m.web.done:
			m.web = nil
		default:
			return m.web.url, m.web.password, nil
		}
	}
	client, err := m.connect(ctx)
	if err != nil {
		return nil, "", err
	}
	version, err := client.health(ctx)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(version, "2.") {
		return nil, "", fmt.Errorf("OpenCode Web requires v2; installed %s: deploy OpenCode again in AI settings", version)
	}
	if m.ctx == nil || m.ctx.Err() != nil {
		return nil, "", fmt.Errorf("AI runtime is not running")
	}
	cfg, err := loadProviderConfig(client.directory)
	if err != nil {
		return nil, "", err
	}
	settings, err := readSettings()
	if err != nil {
		return nil, "", err
	}
	if settings.Model != "" {
		cfg["model"] = settings.Model
	}
	cfg["permission"] = map[string]string{"*": "deny", "panel_audit_execute_sql": "allow", "panel_audit_create_task": "allow"}
	environment, err := m.environment(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("load audit SQL environment: %w", err)
	}
	cfg["agents"] = map[string]any{"build": map[string]any{"system": auditGuidance + environment}}
	cfg["default_agent"] = "build"
	cfg["mcp"] = map[string]any{"servers": map[string]any{"panel_audit": map[string]any{"type": "remote", "url": m.mcpURL, "headers": map[string]string{"Authorization": "Bearer " + m.token}, "oauth": false, "codemode": false}}}
	override, err := json.Marshal(cfg)
	if err != nil {
		return nil, "", err
	}
	processCtx, cancel := context.WithCancel(m.ctx)
	password := randomID() + randomID()
	cmd := client.command(processCtx, "serve", "--stdio", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Env = append(cmd.Env, "OPENCODE_PASSWORD="+password, "OPENCODE_CONFIG_CONTENT="+string(override))
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, "", fmt.Errorf("OpenCode web lease: %w", err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = input.Close()
		return nil, "", fmt.Errorf("OpenCode web output: %w", err)
	}
	tail := &tailBuffer{}
	cmd.Stderr = tail
	if err = cmd.Start(); err != nil {
		cancel()
		_ = input.Close()
		return nil, "", fmt.Errorf("start OpenCode web: %w", err)
	}
	ready := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var response struct {
				URL string `json:"url"`
			}
			line := scanner.Text()
			if json.Unmarshal([]byte(line), &response) == nil && response.URL != "" {
				select {
				case ready <- line:
				default:
				}
			}
		}
		_ = cmd.Wait()
		close(done)
	}()
	fail := func(err error) (*url.URL, string, error) { cancel(); _ = input.Close(); <-done; return nil, "", err }
	timeout := time.NewTimer(45 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case line := <-ready:
			var response struct {
				URL string `json:"url"`
			}
			if json.Unmarshal([]byte(line), &response) != nil || response.URL == "" {
				continue
			}
			target, parseErr := url.Parse(response.URL)
			if parseErr != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.Port() == "" || target.User != nil || target.RawQuery != "" || (target.Path != "" && target.Path != "/") {
				return fail(fmt.Errorf("OpenCode returned invalid loopback endpoint"))
			}
			m.web = &webProcess{cancel: cancel, input: input, done: done, url: target, password: password}
			return target, password, nil
		case <-done:
			cancel()
			_ = input.Close()
			return nil, "", fmt.Errorf("OpenCode web exited before readiness: %s", redactConfigText(client.directory, strings.ReplaceAll(strings.ReplaceAll(tail.String(), password, "[REDACTED]"), m.token, "[REDACTED]")))
		case <-timeout.C:
			return fail(fmt.Errorf("OpenCode web did not become ready within 45 seconds"))
		case <-ctx.Done():
			return fail(fmt.Errorf("start OpenCode web: %w", ctx.Err()))
		}
	}
}

func StripWebCredentials(header http.Header) {
	for _, name := range []string{"Cookie", "Authorization", "Proxy-Authorization", "X-CSRF-Token", "Forwarded", "X-Forwarded-Host", "X-Forwarded-For", "X-Forwarded-Proto"} {
		header.Del(name)
	}
}
