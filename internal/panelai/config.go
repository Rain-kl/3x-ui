package panelai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

var settingsMu sync.Mutex

type AISettings struct {
	// FORK(panel-ai): Connection credentials and paths belong to explicit administrator settings.
	Binary        string `json:"binary"`
	Directory     string `json:"directory"`
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"`
	HasPassword   bool   `json:"hasPassword"`
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

// FORK(panel-ai): Settings responses must never disclose the saved authentication password.
func ReadSettings() (AISettings, error) {
	value, err := readSettings()
	value.HasPassword, value.Password = value.Password != "", ""
	return value, err
}

func readSettings() (AISettings, error) {
	result := AISettings{RetentionDays: 90, Username: "opencode"}
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

// FORK(panel-ai): Persist explicit connection settings and preserve redacted passwords.
func SaveSettings(value AISettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	previous, err := readSettings()
	if err != nil {
		return err
	}
	value.Binary, value.Directory = strings.TrimSpace(value.Binary), strings.TrimSpace(value.Directory)
	value.Username = strings.TrimSpace(value.Username)
	if value.Username == "" {
		value.Username = "opencode"
	}
	if len(value.Password) > 4096 || len(value.Username) > 256 || strings.ContainsAny(value.Username, ":\r\n") {
		return fmt.Errorf("OpenCode username must be at most 256 characters without colon or line breaks; password at most 4096 bytes")
	}
	if value.Password == "" {
		value.Password = previous.Password
	}
	value.HasPassword = false
	for _, path := range []string{value.Binary, value.Directory} {
		if path != "" && (!filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0)) {
			return fmt.Errorf("OpenCode executable and workspace must be absolute paths of at most 4096 bytes")
		}
	}
	value.URL = strings.TrimRight(strings.TrimSpace(value.URL), "/")
	if value.URL != "" {
		if err := validateLocalURL(value.URL); err != nil {
			return err
		}
	}
	if value.Enabled && value.URL == "" {
		return fmt.Errorf("enabling AI requires an OpenCode service URL configured in AI settings")
	}
	if len(value.Model) > 512 || value.RetentionDays < 1 || value.RetentionDays > 3650 {
		return fmt.Errorf("AI model must be at most 512 characters and retention 1..3650 days")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	err = database.GetDB().Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
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
	if previous.Enabled != value.Enabled || previous.URL != value.URL || previous.Binary != value.Binary || previous.Directory != value.Directory || previous.Username != value.Username || previous.Password != value.Password {
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

// FORK(panel-ai): Use saved settings exclusively; do not inspect environment, PATH or user configuration files.
func workspaceDirectory(value AISettings) string {
	if value.Directory != "" {
		return value.Directory
	}
	return filepath.Join(config.GetDBFolderPath(), "ai")
}

func configuredURL(value AISettings) (string, error) {
	if value.URL == "" {
		return "", fmt.Errorf("configure the OpenCode service URL in AI settings")
	}
	if err := validateLocalURL(value.URL); err != nil {
		return "", err
	}
	return strings.TrimRight(value.URL, "/"), nil
}

func filepathAbsolute(path string) (string, error) {
	value, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve OpenCode directory: %w", err)
	}
	return value, nil
}
