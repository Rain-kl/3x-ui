package panelai

import (
	"encoding/json"
	"errors"
	"fmt"
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
	// FORK(panel-ai): The panel owns a local CLI installation rather than HTTP connection credentials.
	Binary        string `json:"binary"`
	Directory     string `json:"directory"`
	Enabled       bool   `json:"enabled"`
	Model         string `json:"model"`
	RetentionDays int    `json:"retentionDays"`
}

type AIStatus struct {
	Connected bool   `json:"connected"`
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

// FORK(panel-ai): Settings omit obsolete HTTP authentication and retain only CLI configuration.
func ReadSettings() (AISettings, error) {
	return readSettings()
}

func readSettings() (AISettings, error) {
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

// FORK(panel-ai): Persist administrator CLI paths; never infer a system installation.
func SaveSettings(value AISettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	previous, err := readSettings()
	if err != nil {
		return err
	}
	value.Binary, value.Directory = strings.TrimSpace(value.Binary), strings.TrimSpace(value.Directory)
	for _, path := range []string{value.Binary, value.Directory} {
		if path != "" && (!filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0)) {
			return fmt.Errorf("OpenCode executable and workspace must be absolute paths of at most 4096 bytes")
		}
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
	if previous.Enabled != value.Enabled || previous.Binary != value.Binary || previous.Directory != value.Directory || previous.Model != value.Model {
		Default.ResetConnection()
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

func filepathAbsolute(path string) (string, error) {
	value, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve OpenCode directory: %w", err)
	}
	return value, nil
}
