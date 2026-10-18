package telegramuser

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type BindCode struct {
	Code      string `json:"code"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt"`
}

func codeHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func CreateCode(clientID int) (BindCode, error) {
	if clientID <= 0 {
		return BindCode{}, fmt.Errorf("binding client ID must be positive")
	}
	var client model.ClientRecord
	if err := database.GetDB().First(&client, clientID).Error; err != nil {
		return BindCode{}, fmt.Errorf("load binding client %d: %w", clientID, err)
	}
	var count int64
	if err := database.GetDB().Model(&model.TelegramBinding{}).Where("client_id = ?", clientID).Count(&count).Error; err != nil {
		return BindCode{}, err
	}
	if count > 0 {
		return BindCode{}, fmt.Errorf("client is already bound; unbind it before generating a new code")
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return BindCode{}, fmt.Errorf("generate Telegram binding code: %w", err)
	}
	code := "bind_" + hex.EncodeToString(secret)
	expires := time.Now().Add(30 * time.Minute).UnixMilli()
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("client_id = ?", clientID).Delete(&model.TelegramBindCode{}).Error; err != nil {
			return err
		}
		return tx.Create(&model.TelegramBindCode{Hash: codeHash(code), ClientID: clientID, ExpiresAt: expires}).Error
	})
	if err != nil {
		return BindCode{}, fmt.Errorf("save Telegram binding code: %w", err)
	}
	cfg, err := Config()
	if err != nil {
		return BindCode{}, err
	}
	link := ""
	if cfg.BotUsername != "" {
		link = "https://t.me/" + cfg.BotUsername + "?start=" + code
	}
	return BindCode{Code: code, URL: link, ExpiresAt: expires}, nil
}

func Bind(tgID int64, proof string) (model.TelegramBinding, error) {
	var binding model.TelegramBinding
	if tgID <= 0 {
		return binding, fmt.Errorf("binding requires a private Telegram account")
	}
	cfg, err := Config()
	if err != nil {
		return binding, err
	}
	proof = strings.TrimSpace(proof)
	if len(proof) > 8192 {
		return binding, fmt.Errorf("binding input exceeds 8192 bytes")
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var clientID int
		if strings.HasPrefix(proof, "bind_") {
			var code model.TelegramBindCode
			if err := tx.Where("hash = ? AND expires_at > ?", codeHash(proof), time.Now().UnixMilli()).First(&code).Error; err != nil {
				return fmt.Errorf("binding code is invalid or expired")
			}
			clientID = code.ClientID
			consumed := tx.Where("hash = ? AND expires_at > ?", code.Hash, time.Now().UnixMilli()).Delete(&model.TelegramBindCode{})
			if consumed.Error != nil || consumed.RowsAffected != 1 {
				return fmt.Errorf("binding code has already been used")
			}
		} else {
			provided, parseErr := url.Parse(proof)
			base, baseErr := url.Parse(cfg.PublicSubURL)
			if parseErr != nil || baseErr != nil || base.Host == "" || provided.User != nil || provided.Scheme != base.Scheme || !strings.EqualFold(provided.Host, base.Host) || !strings.HasPrefix(provided.Path, base.Path) {
				return fmt.Errorf("send a valid subscription link or binding code")
			}
			subID := strings.TrimPrefix(provided.Path, base.Path)
			if subID == "" || strings.Contains(subID, "/") {
				return fmt.Errorf("send a valid subscription link or binding code")
			}
			var clients []model.ClientRecord
			if err := tx.Select("id").Where("sub_id = ?", subID).Limit(2).Find(&clients).Error; err != nil {
				return fmt.Errorf("resolve Telegram subscription binding: %w", err)
			}
			if len(clients) != 1 {
				return fmt.Errorf("subscription link is invalid or ambiguous; request a binding code from the administrator")
			}
			clientID = clients[0].Id
		}
		var client model.ClientRecord
		if err := tx.Select("id", "group_name").First(&client, clientID).Error; err != nil {
			return fmt.Errorf("binding client no longer exists")
		}
		var existing []model.TelegramBinding
		if err := tx.Where("tg_id = ? OR client_id = ?", tgID, clientID).Find(&existing).Error; err != nil {
			return fmt.Errorf("check Telegram binding uniqueness: %w", err)
		}
		if len(existing) > 0 {
			if len(existing) == 1 && existing[0].TgID == tgID && existing[0].ClientID == clientID {
				binding = existing[0]
				return nil
			}
			return fmt.Errorf("account or client is already bound; unbind before changing it")
		}
		binding = model.TelegramBinding{ClientID: clientID, TgID: tgID}
		if err := tx.Create(&binding).Error; err != nil {
			return fmt.Errorf("claim Telegram binding: %w", err)
		}
		return enqueueBindingNotifications(tx, binding, client)
	})
	return binding, err
}

func BoundClient(tgID int64) (*model.ClientRecord, error) {
	var binding model.TelegramBinding
	if err := database.GetDB().Where("tg_id = ?", tgID).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("请先使用 /bind 绑定客户端")
		}
		return nil, fmt.Errorf("load Telegram binding: %w", err)
	}
	var client model.ClientRecord
	if err := database.GetDB().First(&client, binding.ClientID).Error; err != nil {
		return nil, fmt.Errorf("bound client is unavailable; contact the administrator")
	}
	return &client, nil
}

func Unbind(tgID int64) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		var binding model.TelegramBinding
		if err := tx.Where("tg_id = ?", tgID).First(&binding).Error; err != nil {
			return fmt.Errorf("load Telegram binding to remove: %w", err)
		}
		if err := tx.Where("client_id = ?", binding.ClientID).Delete(&model.TelegramBindCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tg_id = ? AND status = ? AND notice_id > 0", tgID, "pending").Model(&model.TelegramMessage{}).Update("status", "cancelled").Error; err != nil {
			return err
		}
		return tx.Delete(&binding).Error
	})
}
