package panelai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

var settingsMu sync.Mutex

type AISettings struct {
	Enabled       bool   `json:"enabled"`
	URL           string `json:"url"`
	Model         string `json:"model"`
	RetentionDays int    `json:"retentionDays"`
}

type AIStatus struct {
	Connected bool   `json:"connected"`
	URL       string `json:"url"`
	Binary    string `json:"binary"`
	Directory string `json:"directory"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

type AIModelOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

func ReadSettings() (AISettings, error) {
	result := AISettings{RetentionDays: 90}
	var row model.Setting
	err := database.GetDB().Where("key = ?", "panelAIConfig").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read AI settings: %w", err)
	}
	if err = json.Unmarshal([]byte(row.Value), &result); err != nil {
		return result, fmt.Errorf("decode AI settings: %w", err)
	}
	if result.RetentionDays < 1 || result.RetentionDays > 3650 {
		return result, fmt.Errorf("stored AI retention must be 1..3650 days")
	}
	return result, nil
}

func SaveSettings(value AISettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	previous, err := ReadSettings()
	if err != nil {
		return err
	}
	value.URL = strings.TrimRight(strings.TrimSpace(value.URL), "/")
	if value.URL != "" {
		if err := validateLocalURL(value.URL); err != nil {
			return err
		}
	}
	if len(value.Model) > 512 || value.RetentionDays < 1 || value.RetentionDays > 3650 {
		return fmt.Errorf("AI model must be at most 512 characters and retention 1..3650 days")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var row model.Setting
		readErr := tx.Where("key = ?", "panelAIConfig").First(&row).Error
		if readErr != nil && !errors.Is(readErr, gorm.ErrRecordNotFound) {
			return readErr
		}
		row.Key, row.Value = "panelAIConfig", string(data)
		return tx.Save(&row).Error
	})
	if err != nil {
		return fmt.Errorf("save AI settings: %w", err)
	}
	if previous.Enabled != value.Enabled || previous.URL != value.URL {
		Default.ResetConnection()
	}
	return nil
}

func validateLocalURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("OpenCode URL must be a local HTTP server root without credentials or query")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("OpenCode must run locally; URL hostname must be localhost or a loopback IP")
	}
	return nil
}

func workspaceDirectory() string {
	if value := os.Getenv("XUI_OPENCODE_DIRECTORY"); value != "" {
		return value
	}
	return filepath.Join(config.GetDBFolderPath(), "ai")
}

func discoverURL(value AISettings) (string, error) {
	for _, raw := range []string{value.URL, os.Getenv("XUI_OPENCODE_URL"), os.Getenv("OPENCODE_SERVER_URL")} {
		if raw != "" {
			if err := validateLocalURL(raw); err != nil {
				return "", err
			}
			return strings.TrimRight(raw, "/"), nil
		}
	}
	home, _ := os.UserHomeDir()
	paths := []string{os.Getenv("OPENCODE_CONFIG"), filepath.Join(os.Getenv("OPENCODE_CONFIG_DIR"), "opencode.json"), filepath.Join(home, ".config", "opencode", "opencode.json")}
	for _, path := range paths {
		if path == "" || path == "opencode.json" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			Server struct {
				Hostname string `json:"hostname"`
				Port     int    `json:"port"`
			} `json:"server"`
		}
		if json.Unmarshal(data, &cfg) == nil && cfg.Server.Port > 0 && cfg.Server.Port <= 65535 {
			raw := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.Server.Port))
			return raw, nil
		}
	}
	return "http://127.0.0.1:4096", nil
}

func discoverBinary() string {
	for _, key := range []string{"XUI_OPENCODE_BIN", "OPENCODE_BIN"} {
		if value := os.Getenv(key); value != "" {
			if path, err := exec.LookPath(value); err == nil {
				return path
			}
			return value
		}
	}
	if path, err := exec.LookPath("opencode"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{filepath.Join(home, ".opencode", "bin", "opencode"), filepath.Join(home, ".local", "bin", "opencode"), filepath.Join("node_modules", ".bin", "opencode")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			absolute, _ := filepath.Abs(path)
			return absolute
		}
	}
	return ""
}

func filepathAbsolute(path string) (string, error) {
	value, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve OpenCode directory: %w", err)
	}
	return value, nil
}
