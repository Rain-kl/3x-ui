package telegramuser

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type CustomCommands struct {
	Commands []model.TelegramCustomCommand `json:"commands"`
}

var customCommandName = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func ReadCustomCommands() (CustomCommands, error) {
	result := CustomCommands{Commands: []model.TelegramCustomCommand{}}
	if err := database.GetDB().Order("sort_order ASC, command ASC").Find(&result.Commands).Error; err != nil {
		return result, fmt.Errorf("read Telegram custom commands: %w", err)
	}
	return result, nil
}

// FORK(telegram-subscription-details): Reserve a native menu slot and command name for /nodes.
func SaveCustomCommands(value CustomCommands) (CustomCommands, error) {
	if len(value.Commands) > 93 {
		return CustomCommands{}, fmt.Errorf("at most 93 Telegram custom commands are allowed")
	}
	seen := map[string]bool{"nodes": true, "start": true, "help": true, "subscription": true, "reset": true, "usage": true, "notifications": true, "bind": true, "unbind": true}
	for i := range value.Commands {
		row := &value.Commands[i]
		row.Command = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(row.Command), "/"))
		row.Description = strings.TrimSpace(row.Description)
		if !customCommandName.MatchString(row.Command) || seen[row.Command] {
			return CustomCommands{}, fmt.Errorf("custom command %d: name must be unique, non-reserved and contain 1–32 lowercase ASCII letters, digits or underscores", i+1)
		}
		if n := utf8.RuneCountInString(row.Description); n < 1 || n > 256 {
			return CustomCommands{}, fmt.Errorf("custom command /%s: menu description must contain 1–256 characters", row.Command)
		}
		if strings.TrimSpace(row.Reply) == "" || utf8.RuneCountInString(row.Reply) > 4096 {
			return CustomCommands{}, fmt.Errorf("custom command /%s: reply must contain 1–4096 characters", row.Command)
		}
		row.SortOrder = i
		seen[row.Command] = true
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.TelegramCustomCommand{}).Error; err != nil {
			return fmt.Errorf("replace Telegram custom commands: %w", err)
		}
		if len(value.Commands) > 0 {
			if err := tx.Create(&value.Commands).Error; err != nil {
				return fmt.Errorf("save Telegram custom commands: %w", err)
			}
		}
		return tx.Create(&model.TelegramLog{Event: "commands.updated", Detail: fmt.Sprintf("custom commands=%d", len(value.Commands))}).Error
	})
	if err != nil {
		return CustomCommands{}, fmt.Errorf("update Telegram custom commands: %w", err)
	}
	if value.Commands == nil {
		value.Commands = []model.TelegramCustomCommand{}
	}
	return value, nil
}

func (r *receiver) syncCommands() {
	value, err := ReadCustomCommands()
	if err != nil {
		r.failure("bot.commands.failed", err)
		return
	}
	commands := []map[string]string{{"command": "nodes", "description": "获取全部订阅节点名"}, {"command": "notifications", "description": "获取全部通知"}, {"command": "subscription", "description": "获取订阅链接"}, {"command": "reset", "description": "重置订阅链接"}, {"command": "usage", "description": "查询剩余流量和天数"}, {"command": "bind", "description": "绑定客户端"}, {"command": "unbind", "description": "解绑客户端"}}
	for _, row := range value.Commands {
		if row.Enabled && row.Command != "nodes" {
			commands = append(commands, map[string]string{"command": row.Command, "description": row.Description})
		}
	}
	// FORK(telegram-subscription-details): Old 94-item configurations must not break the 100-command Telegram limit.
	truncated := len(commands) > 100
	if truncated {
		commands = commands[:100]
	}
	encoded, err := json.Marshal(commands)
	if err != nil {
		r.failure("bot.commands.failed", fmt.Errorf("encode Telegram command menu: %w", err))
		return
	}
	if r.commandMenu == string(encoded) {
		return
	}
	if truncated {
		r.log(0, 0, "bot.commands.limit", "only the first 93 enabled custom commands fit alongside built-in commands; remove excess entries")
	}
	if err := r.user.call(r.ctx, "setMyCommands", map[string]any{"commands": commands}, nil); err != nil {
		r.failure("bot.commands.failed", err)
		return
	}
	r.commandMenu = string(encoded)
}

func (r *receiver) customCommand(tgID int64, name string) bool {
	var row model.TelegramCustomCommand
	err := database.GetDB().Where("command = ? AND enabled = ?", strings.TrimPrefix(name, "/"), true).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		r.log(tgID, 0, "command.custom.failed", fmt.Sprintf("load %s: %v", name, err))
		r.reply(tgID, "读取自定义指令失败，请稍后重试或联系管理员。", nil)
		return true
	}
	r.reply(tgID, row.Reply, nil)
	return true
}
