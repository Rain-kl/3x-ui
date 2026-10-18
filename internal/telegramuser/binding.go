package telegramuser

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

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

// FORK(telegram-invite-management): Keep one current invitation available for administrator review.
var inviteMu sync.Mutex

func CreateCode(clientID int) (BindCode, error) {
	inviteMu.Lock()
	defer inviteMu.Unlock()
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
	err := database.GetDB().Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("client_id = ?", clientID).Delete(&model.TelegramBindCode{}).Error; err != nil {
			return err
		}
		return tx.Create(&model.TelegramBindCode{Code: code, Hash: codeHash(code), ClientID: clientID, ExpiresAt: expires}).Error
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
	inviteMu.Lock()
	defer inviteMu.Unlock()
	var binding model.TelegramBinding
	if tgID <= 0 {
		return binding, fmt.Errorf("binding requires a private Telegram account")
	}
	proof = strings.TrimSpace(proof)
	if len(proof) > 8192 {
		return binding, fmt.Errorf("binding input exceeds 8192 bytes")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var clientID int
		if strings.HasPrefix(proof, "bind_") {
			var code model.TelegramBindCode
			if err := tx.Where("hash = ? AND expires_at > ? AND used_at = 0 AND revoked_at = 0", codeHash(proof), time.Now().UnixMilli()).First(&code).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("绑定码无效、已轮转、已使用或已过期，请联系管理员生成新码")
				}
				return fmt.Errorf("load binding code: %w", err)
			}
			clientID = code.ClientID
			consumed := tx.Where("hash = ? AND expires_at > ? AND used_at = 0 AND revoked_at = 0", code.Hash, time.Now().UnixMilli()).Model(&model.TelegramBindCode{}).Update("used_at", time.Now().UnixMilli())
			if consumed.Error != nil {
				return fmt.Errorf("consume binding code: %w", consumed.Error)
			}
			if consumed.RowsAffected != 1 {
				return fmt.Errorf("binding code has already been used")
			}
		} else {
			ids, err := matchSubIDs(tx, proof)
			if err != nil {
				return fmt.Errorf("resolve Telegram subscription binding: %w", err)
			}
			switch len(ids) {
			case 0:
				return fmt.Errorf("未在消息中找到订阅凭据，请发送完整订阅链接或绑定码")
			case 1:
				clientID = ids[0]
			default:
				return fmt.Errorf("消息匹配到多个客户端的订阅凭据，请只发送一个或改用绑定码")
			}
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
		if err := tx.Model(&model.TelegramBindCode{}).Where("client_id = ? AND used_at = 0 AND revoked_at = 0", clientID).Update("used_at", time.Now().UnixMilli()).Error; err != nil {
			return fmt.Errorf("finish invitation lifecycle: %w", err)
		}
		_, err := enqueueClientNotifications(tx, binding, client)
		return err
	})
	return binding, err
}

// matchSubIDs returns clients whose subscription ID appears verbatim in text;
// the sub ID itself is the bearer credential, so containment authorizes binding.
func matchSubIDs(tx *gorm.DB, text string) ([]int, error) {
	var clients []model.ClientRecord
	if err := tx.Select("id", "sub_id").Where("sub_id <> ''").Find(&clients).Error; err != nil {
		return nil, err
	}
	var ids []int
	for _, client := range clients {
		if client.SubID != "" && strings.Contains(text, client.SubID) {
			ids = append(ids, client.Id)
		}
	}
	return ids, nil
}

func hasSubID(text string) (bool, error) {
	ids, err := matchSubIDs(database.GetDB(), text)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
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
