package panelai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type openCode struct{ binary, directory string }

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

func (o *openCode) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, o.binary, args...)
	cmd.Dir = o.directory
	cmd.Env = cliEnvironment(o.directory)
	if cfg, err := loadProviderConfig(o.directory); err == nil {
		if data, err := json.Marshal(cfg); err == nil {
			cmd.Env = append(cmd.Env, "OPENCODE_CONFIG_CONTENT="+string(data))
		}
	}
	cmd.WaitDelay = 2 * time.Second
	isolateProcess(cmd)
	return cmd
}

// FORK(panel-ai): Isolate native configuration and credentials from the administrator's shell environment.
func cliEnvironment(directory string) []string {
	allowed := map[string]bool{"PATH": true, "SystemRoot": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "TEMP": true, "TMP": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true}
	env := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allowed[key] {
			env = append(env, item)
		}
	}
	root := filepath.Join(directory, ".runtime")
	for key, suffix := range map[string]string{"HOME": "home", "USERPROFILE": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state"} {
		env = append(env, key+"="+filepath.Join(root, suffix))
	}
	return append(env, "NO_COLOR=1", "TERM=dumb", "OPENCODE_CONFIG="+filepath.Join(directory, "opencode.json"), "OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_PROJECT_CONFIG=true", "OPENCODE_DISABLE_CLAUDE_CODE=true", "OPENCODE_DISABLE_EXTERNAL_SKILLS=true")
}

func (o *openCode) output(ctx context.Context, args ...string) (string, error) {
	cmd := o.command(ctx, args...)
	var stdout bytes.Buffer
	tail := &tailBuffer{}
	cmd.Stdout = &boundedOutput{buffer: &stdout, limit: 4 << 20}
	cmd.Stderr = tail
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("OpenCode CLI %s: %w; %s", strings.Join(args, " "), err, redactConfigText(o.directory, tail.String()))
	}
	return stdout.String(), nil
}

type boundedOutput struct {
	buffer *bytes.Buffer
	limit  int
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > w.limit {
		return 0, fmt.Errorf("OpenCode CLI output exceeds %d bytes", w.limit)
	}
	return w.buffer.Write(p)
}

func (o *openCode) health(ctx context.Context) (string, error) {
	out, err := o.output(ctx, "--version")
	return strings.TrimPrefix(strings.TrimSpace(out), "opencode v"), err
}

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
	binary := cfg.Binary
	if binary == "" {
		return nil, fmt.Errorf("OpenCode CLI is not installed; use Deploy OpenCode in AI settings")
	}
	directory, err := filepathAbsolute(workspaceDirectory(cfg))
	if err != nil {
		return nil, err
	}
	if err = prepareWorkspace(directory); err != nil {
		return nil, err
	}
	client := &openCode{binary: binary, directory: directory}
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err = client.health(probe); err != nil {
		return nil, err
	}
	m.client = client
	return client, nil
}

func prepareWorkspace(directory string) error {
	for _, suffix := range []string{"", ".runtime/home", ".runtime/config", ".runtime/data", ".runtime/cache", ".runtime/state"} {
		if err := os.MkdirAll(filepath.Join(directory, suffix), 0o700); err != nil {
			return fmt.Errorf("prepare OpenCode workspace %s: %w", directory, err)
		}
	}
	return nil
}

func (m *Manager) ConnectionStatus(ctx context.Context) AIStatus {
	cfg, err := ReadSettings()
	status := AIStatus{Binary: cfg.Binary, Directory: workspaceDirectory(cfg)}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	if !cfg.Enabled {
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
	config, err := loadProviderConfig(client.directory)
	if err != nil {
		return nil, err
	}
	providers, _ := config["providers"].(map[string]any)
	if len(providers) == 0 {
		return []AIModelOption{}, nil
	}
	out, err := client.output(ctx, "models", "--standalone")
	if err != nil {
		return nil, err
	}
	result := []AIModelOption{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		provider, id, ok := strings.Cut(line, "/")
		if ok && providers[provider] != nil && id != "" {
			result = append(result, AIModelOption{ID: line, Name: id, Provider: provider})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
