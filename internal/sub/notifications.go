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
		if notice.TargetType == "clients" {
			for _, id := range notice.ClientIDs {
				if clientIDs[id] {
					matched = true
					break
				}
			}
		} else if notice.TargetType == "groups" {
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
	text := []rune(strings.Join(strings.Fields(notice.Title+" · "+notice.Body), " "))
	if len(text) > 180 {
		text = append(text[:180], '…')
	}
	return fmt.Sprintf("📢 %s (#%d)", string(text), notice.Id)
}

func noticeLinks(notices []SubscriptionNotice) []string {
	result := make([]string, 0, len(notices))
	for _, notice := range notices {
		if notice.PublishToNodes {
			result = append(result, "socks://notification.invalid:1#"+url.PathEscape(noticeNodeName(notice)))
		}
	}
	return result
}

func (s *SubJsonService) noticeConfig(notice SubscriptionNotice) json_util.RawMessage {
	config := make(map[string]any)
	maps.Copy(config, s.bakedTemplate())
	config["remarks"] = noticeNodeName(notice)
	config["outbounds"] = []any{map[string]any{"tag": "notification", "protocol": "blackhole"}}
	config["routing"] = map[string]any{"rules": []any{map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "notification"}}}
	raw, _ := json.Marshal(config)
	return raw
}

func isNotificationProxy(proxy map[string]any) bool {
	return proxy["server"] == "notification.invalid" && proxy["port"] == 1
}
