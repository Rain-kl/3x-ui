package sub

import (
	"fmt"
	"maps"
	"net/url"
	"strings"

	"github.com/goccy/go-json"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
)

type SubscriptionNotice struct {
	Id             int    `json:"id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	PublishedAt    int64  `json:"publishedAt"`
	PublishToNodes bool   `json:"publishToNodes"`
}

const (
	noticeNodeUUID = "00000000-0000-4000-8000-000000000001"
	noticeNodeHost = "127.0.0.1"
	noticeNodePort = 1
)

func subscriptionNotices(subID string) ([]SubscriptionNotice, error) {
	result := []SubscriptionNotice{}
	var notices []model.SubscriptionNotification
	if err := database.GetDB().Where("published = ?", true).Order("sort_index, id").Find(&notices).Error; err != nil {
		return nil, fmt.Errorf("load published subscription notifications: %w", err)
	}
	if len(notices) == 0 {
		return result, nil
	}
	var clients []model.ClientRecord
	if err := database.GetDB().Select("id, group_name").Where("sub_id = ?", subID).Find(&clients).Error; err != nil {
		return nil, fmt.Errorf("load notification audience for subscription: %w", err)
	}
	clientIDs := make(map[int]bool, len(clients))
	groups := make(map[string]bool, len(clients))
	for _, client := range clients {
		clientIDs[client.Id] = true
		if client.Group != "" {
			groups[client.Group] = true
		}
	}
	for _, notice := range notices {
		matched := false
		if notice.TargetType == "clients" || notice.TargetType == "both" {
			for _, id := range notice.ClientIDs {
				if clientIDs[id] {
					matched = true
					break
				}
			}
		}
		if !matched && (notice.TargetType == "groups" || notice.TargetType == "both") {
			for _, group := range notice.Groups {
				if groups[group] {
					matched = true
					break
				}
			}
		}
		if matched {
			result = append(result, SubscriptionNotice{Id: notice.Id, Title: notice.Title, Body: notice.Body, PublishedAt: notice.PublishedAt, PublishToNodes: notice.PublishToNodes})
		}
	}
	return result, nil
}

func noticeNodeName(notice SubscriptionNotice) string {
	label := notice.Body
	if notice.Title != "" {
		label = notice.Title + " · " + notice.Body
	}
	text := []rune(strings.Join(strings.Fields(label), " "))
	if len(text) > 180 {
		text = append(text[:180], '…')
	}
	return fmt.Sprintf("📢 %s (#%d)", string(text), notice.Id)
}

func noticeLinks(notices []SubscriptionNotice) []string {
	result := make([]string, 0, len(notices))
	for _, notice := range notices {
		if notice.PublishToNodes {
			result = append(result, fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=tcp&security=none#%s", noticeNodeUUID, noticeNodeHost, noticeNodePort, url.PathEscape(noticeNodeName(notice))))
		}
	}
	return result
}

func (s *SubJsonService) noticeConfig(notice SubscriptionNotice) json_util.RawMessage {
	config := make(map[string]any)
	maps.Copy(config, s.bakedTemplate())
	config["remarks"] = noticeNodeName(notice)
	config["outbounds"] = []any{
		map[string]any{"tag": "proxy", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{
			"address": noticeNodeHost, "port": noticeNodePort,
			"users": []any{map[string]any{"id": noticeNodeUUID, "encryption": "none"}},
		}}}, "streamSettings": map[string]any{"network": "tcp", "security": "none"}},
		map[string]any{"tag": "notification", "protocol": "blackhole"},
	}
	config["routing"] = map[string]any{"rules": []any{map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "notification"}}}
	raw, _ := json.Marshal(config)
	return raw
}

func isNotificationProxy(proxy map[string]any) bool {
	return proxy["type"] == "vless" && proxy["uuid"] == noticeNodeUUID && proxy["server"] == noticeNodeHost && proxy["port"] == noticeNodePort
}
