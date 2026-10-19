package panelai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var providerConfigMu sync.Mutex

type AIProviderConfig struct {
	Config string `json:"config"`
	Path   string `json:"path"`
}

func loadProviderConfig(directory string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(directory, "opencode.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"$schema": "https://opencode.ai/config.json", "providers": map[string]any{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OpenCode provider configuration: %w", err)
	}
	if len(data) > 128<<10 {
		return nil, fmt.Errorf("OpenCode provider configuration exceeds 128 KiB")
	}
	var cfg map[string]any
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode OpenCode provider configuration: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("OpenCode configuration must be a JSON object")
	}
	return normalizeProviderConfig(cfg), nil
}

func secretField(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	return strings.Contains(key, "apikey") || strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "secret") || key == "authorization" || key == "cookie"
}

func ReadProviderConfig() (AIProviderConfig, error) {
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()
	cfg, err := readSettings()
	if err != nil {
		return AIProviderConfig{}, err
	}
	directory := workspaceDirectory(cfg)
	value, err := loadProviderConfig(directory)
	if err != nil {
		return AIProviderConfig{}, err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	return AIProviderConfig{Config: string(data), Path: filepath.Join(directory, "opencode.json")}, err
}

func SaveProviderConfig(input AIProviderConfig) (AIProviderConfig, error) {
	providerConfigMu.Lock()
	err := saveProviderConfig(input.Config)
	providerConfigMu.Unlock()
	if err != nil {
		return AIProviderConfig{}, err
	}
	Default.ResetConnection()
	return ReadProviderConfig()
}

func saveProviderConfig(raw string) error {
	if len(raw) > 128<<10 {
		return fmt.Errorf("OpenCode configuration exceeds 128 KiB")
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode OpenCode JSON: %w", err)
	}
	if !json.Valid([]byte(raw)) || value == nil {
		return fmt.Errorf("OpenCode configuration must be one JSON object")
	}
	for key := range value {
		if key != "$schema" && key != "providers" && key != "provider" && key != "model" && key != "small_model" {
			return fmt.Errorf("OpenCode configuration field %q is panel-managed; only providers/model/small_model are editable", key)
		}
	}
	for _, key := range []string{"model", "small_model", "$schema"} {
		if v, ok := value[key]; ok {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("OpenCode %s must be a string", key)
			}
		}
	}
	value = normalizeProviderConfig(value)
	providers, ok := value["providers"].(map[string]any)
	if !ok {
		return fmt.Errorf("OpenCode providers must be an object")
	}
	for id, entry := range providers {
		provider, ok := entry.(map[string]any)
		if !ok || id == "" || strings.ContainsAny(id, "/\\\n\r") {
			return fmt.Errorf("invalid OpenCode provider ID or object")
		}
		if adapter, ok := provider["package"]; ok {
			pkg, ok := adapter.(string)
			if !ok || !strings.HasPrefix(pkg, "@opencode/ai/providers/") || strings.ContainsAny(strings.TrimPrefix(pkg, "@opencode/ai/providers/"), "/@ \\;\n\r") {
				return fmt.Errorf("provider %q package must be an @opencode/ai/providers adapter", id)
			}
		}
		if options, ok := provider["settings"]; ok {
			if _, ok := options.(map[string]any); !ok {
				return fmt.Errorf("provider %q settings must be an object", id)
			}
		}
		if models, ok := provider["models"]; ok {
			if _, ok := models.(map[string]any); !ok {
				return fmt.Errorf("provider %q models must be an object", id)
			}
		}
	}
	if bytes.Contains([]byte(raw), []byte("{file:")) || bytes.Contains([]byte(raw), []byte("{env:")) {
		return fmt.Errorf("OpenCode file/environment substitutions are not supported; enter provider settings directly")
	}
	cfg, err := readSettings()
	if err != nil {
		return err
	}
	directory := workspaceDirectory(cfg)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(directory, "opencode.json", data)
}

func writePrivateFile(directory, name string, data []byte) error {
	if err := prepareWorkspace(directory); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".opencode-*")
	if err != nil {
		return fmt.Errorf("create OpenCode configuration: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write OpenCode configuration: %w", err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync OpenCode configuration: %w", err)
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), filepath.Join(directory, name)); err != nil {
		return fmt.Errorf("install OpenCode configuration: %w", err)
	}
	return nil
}

func redactConfigText(directory, text string) string {
	config, err := loadProviderConfig(directory)
	if err != nil {
		return "OpenCode command failed; provider configuration could not be read for safe diagnostics"
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if secretField(key) {
					if s, ok := item.(string); ok && s != "" {
						text = strings.ReplaceAll(text, s, "[REDACTED]")
					}
				} else {
					walk(item)
				}
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(config)
	return text
}

func normalizeProviderConfig(value map[string]any) map[string]any {
	if _, exists := value["providers"]; exists {
		return value
	}
	providers, ok := value["provider"].(map[string]any)
	if !ok {
		return value
	}
	for _, entry := range providers {
		provider, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if pkg, ok := provider["npm"].(string); ok {
			provider["package"] = strings.Replace(pkg, "@ai-sdk/", "@opencode/ai/providers/", 1)
			delete(provider, "npm")
		}
		if options, ok := provider["options"]; ok {
			provider["settings"] = options
			delete(provider, "options")
		}
	}
	value["providers"] = providers
	delete(value, "provider")
	return value
}
