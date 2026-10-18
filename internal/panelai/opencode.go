package panelai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type openCode struct {
	base, directory, username, password string
	http                                *http.Client
}

type openCodeHTTPError struct {
	Path   string
	Status int
	Detail string
}

func (e *openCodeHTTPError) Error() string {
	return fmt.Sprintf("OpenCode %s returned HTTP %d: %s", e.Path, e.Status, e.Detail)
}

type tailBuffer struct {
	mu   sync.Mutex
	text string
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > 8192 {
		b.text = b.text[len(b.text)-8192:]
	}
	return len(p), nil
}
func (b *tailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.text }

// FORK(panel-ai): Use only the credentials explicitly saved in the panel.
func newOpenCode(base, directory, username, password string) *openCode {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &openCode{base: base, directory: directory, username: username, password: password, http: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("OpenCode redirects are not allowed")
	}}}
}

func (o *openCode) call(ctx context.Context, method, path string, body, result any) error {
	var encoded io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode OpenCode request: %w", err)
		}
		encoded = bytes.NewReader(data)
	}
	endpoint := o.base + path
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	endpoint += separator + "directory=" + url.QueryEscape(o.directory)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, encoded)
	if err != nil {
		return fmt.Errorf("build OpenCode %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.password != "" {
		req.SetBasicAuth(o.username, o.password)
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return fmt.Errorf("OpenCode %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return fmt.Errorf("read OpenCode %s: %w", path, err)
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("OpenCode %s response exceeded 8 MiB", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(data[:min(len(data), 2048)])
		if o.password != "" {
			detail = strings.ReplaceAll(detail, o.password, "[REDACTED]")
		}
		return &openCodeHTTPError{Path: path, Status: resp.StatusCode, Detail: detail}
	}
	if result != nil && len(data) > 0 {
		if err = json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("decode OpenCode %s: %w", path, err)
		}
	}
	return nil
}

func (o *openCode) health(ctx context.Context) (string, error) {
	var status struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := o.call(ctx, "GET", "/global/health", nil, &status); err != nil {
		return "", err
	}
	if !status.Healthy {
		return "", fmt.Errorf("OpenCode reported an unhealthy server")
	}
	return status.Version, nil
}

// FORK(panel-ai): Connection and startup paths must come from saved administrator settings.
func (m *Manager) connect(ctx context.Context) (*openCode, error) {
	m.connectionMu.Lock()
	defer m.connectionMu.Unlock()
	if m.client != nil {
		return m.client, nil
	}
	cfg, err := readSettings()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("AI is disabled; enable it in AI settings")
	}
	base, err := configuredURL(cfg)
	if err != nil {
		return nil, err
	}
	directory, err := filepathAbsolute(workspaceDirectory(cfg))
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create OpenCode workspace: %w", err)
	}
	client := newOpenCode(base, directory, cfg.Username, cfg.Password)
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = client.health(probe)
	cancel()
	if err != nil {
		var httpErr *openCodeHTTPError
		if errors.As(err, &httpErr) {
			return nil, fmt.Errorf("existing OpenCode health check failed: %w", err)
		}
		binary := cfg.Binary
		if binary == "" {
			return nil, fmt.Errorf("OpenCode is unavailable at %s and no executable path is configured; set it in AI settings or start the configured local service: %w", base, err)
		}
		parsed, _ := url.Parse(base)
		port := parsed.Port()
		if port == "" {
			port = "80"
		}
		processCtx, processCancel := context.WithCancel(m.ctx)
		password := cfg.Password
		cmd := exec.CommandContext(processCtx, binary, "serve", "--hostname", parsed.Hostname(), "--port", port)
		cmd.Dir = directory
		// FORK(panel-ai): OpenCode receives the saved credentials, independent of the panel's inherited environment.
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "OPENCODE_SERVER_PASSWORD=") && !strings.HasPrefix(item, "OPENCODE_SERVER_USERNAME=") {
				cmd.Env = append(cmd.Env, item)
			}
		}
		cmd.Env = append(cmd.Env, "OPENCODE_SERVER_PASSWORD="+password, "OPENCODE_SERVER_USERNAME="+cfg.Username, "OPENCODE_DISABLE_AUTOUPDATE=true")
		tail := &tailBuffer{}
		cmd.Stdout = tail
		cmd.Stderr = tail
		if err = cmd.Start(); err != nil {
			processCancel()
			return nil, fmt.Errorf("start OpenCode %s: %w", binary, err)
		}
		m.processCancel = processCancel
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		client = newOpenCode(base, directory, cfg.Username, password)
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
	ready:
		for {
			select {
			case <-ctx.Done():
				processCancel()
				m.processCancel = nil
				return nil, ctx.Err()
			case exitErr := <-exited:
				processCancel()
				m.processCancel = nil
				return nil, fmt.Errorf("OpenCode exited during startup (%w): %s", exitErr, redactOpenCodeOutput(tail.String(), password))
			case <-deadline.C:
				processCancel()
				m.processCancel = nil
				return nil, fmt.Errorf("OpenCode startup timed out: %s", redactOpenCodeOutput(tail.String(), password))
			case <-tick.C:
				probe, cancel := context.WithTimeout(ctx, time.Second)
				_, err = client.health(probe)
				cancel()
				if err == nil {
					break ready
				}
			}
		}
	}
	if err = m.registerMCP(ctx, client); err != nil {
		if m.processCancel != nil {
			m.processCancel()
			m.processCancel = nil
		}
		return nil, err
	}
	m.client = client
	return client, nil
}

// FORK(panel-ai): Report configured paths without scanning the host environment.
func (m *Manager) ConnectionStatus(ctx context.Context) AIStatus {
	cfg, err := ReadSettings()
	status := AIStatus{}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	directory, dirErr := filepathAbsolute(workspaceDirectory(cfg))
	status.Binary, status.Directory, status.URL = cfg.Binary, directory, cfg.URL
	if dirErr != nil {
		status.Error = dirErr.Error()
		return status
	}
	if !cfg.Enabled {
		return status
	}
	status.URL, err = configuredURL(cfg)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	client, err := m.connect(ctx)
	if err == nil {
		status.Version, err = client.health(ctx)
	}
	status.Connected = err == nil
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func (m *Manager) Models(ctx context.Context) ([]AIModelOption, error) {
	client, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	var list struct {
		All []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				Name string `json:"name"`
			} `json:"models"`
		} `json:"all"`
		Connected []string `json:"connected"`
	}
	if err = client.call(ctx, "GET", "/provider", nil, &list); err != nil {
		return nil, err
	}
	connected := map[string]bool{}
	for _, id := range list.Connected {
		connected[id] = true
	}
	result := []AIModelOption{}
	for _, provider := range list.All {
		if connected[provider.ID] {
			for id, item := range provider.Models {
				result = append(result, AIModelOption{ID: provider.ID + "/" + id, Name: item.Name, Provider: provider.ID})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func redactOpenCodeOutput(value, password string) string {
	if password == "" {
		return value
	}
	return strings.ReplaceAll(value, password, "[REDACTED]")
}
