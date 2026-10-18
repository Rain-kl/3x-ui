package telegramuser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/mymmrac/telego"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func messageFromTelegram(message *telego.Message) model.TelegramMessage {
	row := model.TelegramMessage{ExternalID: message.MessageID, Kind: "text", Text: message.Text}
	if row.Text == "" {
		row.Text = message.Caption
	}
	switch {
	case len(message.Photo) > 0:
		photo := message.Photo[len(message.Photo)-1]
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "photo", photo.FileID, int64(photo.FileSize), "photo.jpg", "image/jpeg"
	case message.Animation != nil:
		file := message.Animation
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "animation", file.FileID, file.FileSize, file.FileName, file.MimeType
	case message.Document != nil:
		file := message.Document
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "document", file.FileID, file.FileSize, file.FileName, file.MimeType
	case message.Video != nil:
		file := message.Video
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "video", file.FileID, file.FileSize, file.FileName, file.MimeType
	case message.Audio != nil:
		file := message.Audio
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "audio", file.FileID, file.FileSize, file.FileName, file.MimeType
	case message.Voice != nil:
		file := message.Voice
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "voice", file.FileID, file.FileSize, "voice.ogg", file.MimeType
	case message.VideoNote != nil:
		file := message.VideoNote
		row.Kind, row.FileID, row.FileSize, row.FileName, row.MIME = "video_note", file.FileID, int64(file.FileSize), "video-note.mp4", "video/mp4"
	case message.Sticker != nil:
		file := message.Sticker
		row.Kind, row.FileID, row.FileSize, row.FileName = "sticker", file.FileID, int64(file.FileSize), "sticker.webp"
		if file.IsVideo {
			row.FileName = "sticker.webm"
		} else if file.IsAnimated {
			row.FileName = "sticker.tgs"
		}
	case message.Contact != nil:
		row.Kind = "contact"
		row.Text = fmt.Sprintf("联系人：%s %s\n%s", message.Contact.FirstName, message.Contact.LastName, message.Contact.PhoneNumber)
	case message.Location != nil:
		row.Kind = "location"
		row.Text = fmt.Sprintf("位置：%.7f, %.7f", message.Location.Latitude, message.Location.Longitude)
	default:
		if row.Text == "" {
			row.Kind = "other"
			raw, err := json.Marshal(message)
			if err == nil {
				row.Text = boundedText(string(raw), 16000)
			}
		}
	}
	return row
}

func mediaMethod(kind string) (string, string) {
	switch kind {
	case "photo":
		return "sendPhoto", "photo"
	case "video":
		return "sendVideo", "video"
	case "audio":
		return "sendAudio", "audio"
	case "voice":
		return "sendVoice", "voice"
	case "sticker":
		return "sendSticker", "sticker"
	case "animation":
		return "sendAnimation", "animation"
	case "video_note":
		return "sendVideoNote", "video_note"
	default:
		return "sendDocument", "document"
	}
}

func (m *Manager) Upload(ctx context.Context, tgID int64, name, contentType, caption string, data []byte, replyID int64) (model.TelegramMessage, error) {
	var row model.TelegramMessage
	r, err := m.receiver()
	if err != nil {
		return row, err
	}
	if len(data) == 0 || len(data) > MaxAttachment || len(utf16.Encode([]rune(caption))) > 900 {
		return row, fmt.Errorf("upload requires 1–20 MiB and a caption of at most 900 characters")
	}
	var conversation model.TelegramConversation
	if err := database.GetDB().First(&conversation, "tg_id = ?", tgID).Error; err != nil {
		return row, fmt.Errorf("load upload conversation: %w", err)
	}
	if replyID > 0 {
		var original model.TelegramMessage
		if err := database.GetDB().First(&original, "id = ? AND tg_id = ? AND delivery_bot <> ?", replyID, tgID, "admin").Error; err != nil {
			return row, fmt.Errorf("attachment reply message is not in this conversation")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer func() { stop(); cancel() }()
	kind := "document"
	if strings.HasPrefix(contentType, "image/") && contentType != "image/svg+xml" {
		kind = "photo"
	} else if strings.HasPrefix(contentType, "video/") {
		kind = "video"
	} else if strings.HasPrefix(contentType, "audio/") {
		kind = "audio"
	}
	row = model.TelegramMessage{TgID: tgID, TargetChatID: tgID, Direction: "outgoing", Role: "web", Kind: kind, Text: caption, FileName: name, FileSize: int64(len(data)), MIME: contentType, SourceBot: "user", DeliveryBot: "user", Status: "sending", ReplyID: replyID}
	if client, err := BoundClient(tgID); err == nil {
		row.ClientID = client.Id
	}
	if _, err := recordMessage(&row, "", ""); err != nil {
		return row, err
	}
	params := map[string]any{"chat_id": tgID, "caption": caption}
	if replyID > 0 {
		var original model.TelegramMessage
		if err := database.GetDB().First(&original, "id = ? AND tg_id = ? AND delivery_bot <> ?", replyID, tgID, "admin").Error; err == nil && original.ExternalID > 0 {
			params["reply_parameters"] = map[string]any{"message_id": original.ExternalID, "allow_sending_without_reply": true}
		}
	}
	method, field := mediaMethod(kind)
	var sent telego.Message
	sendErr := r.user.upload(ctx, method, field, name, data, params, &sent)
	media := messageFromTelegram(&sent)
	if sendErr == nil {
		row.FileID = media.FileID
	}
	r.complete(&row, sent, sendErr, false)
	if err := database.GetDB().First(&row, row.Id).Error; err != nil {
		return row, err
	}
	return row, sendErr
}
