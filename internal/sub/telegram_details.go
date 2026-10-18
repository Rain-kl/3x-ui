package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/telegramuser"
)

func (p *LinkProvider) BotSubscriptionDetails(host, subID string) (telegramuser.SubscriptionDetails, error) {
	result := telegramuser.SubscriptionDetails{Nodes: []string{}}
	if subID == "" {
		return result, fmt.Errorf("subscription identifier is required for Bot details")
	}
	svc := p.build(host)
	svc.botNodeNames = &result.Nodes
	if _, _, _, _, err := svc.getSubs(subID); err != nil {
		return result, fmt.Errorf("render Bot subscription nodes: %w", err)
	}
	mixed, err := svc.manualMixedNodes(subID)
	if err != nil {
		return result, err
	}
	var blocks []string
	for _, node := range mixed {
		result.Nodes = append(result.Nodes, strings.Join(strings.Fields(node.Name), " "))
		blocks = append(blocks, fmt.Sprintf("%s\nHTTP: %s\nSOCKS5: %s\n用户名: %s\n密码: %s", node.Name, node.HTTP, node.SOCKS5, node.Username, node.Password))
	}
	result.Mixed = strings.Join(blocks, "\n\n")
	return result, nil
}

func (s *SubService) captureBotNodeNames(raw, fallback string) {
	if s.botNodeNames == nil {
		return
	}
	for _, line := range splitLinkLines(raw) {
		name := ""
		parsed, err := url.Parse(line)
		if err == nil {
			name = parsed.Fragment
		}
		if strings.HasPrefix(line, "vmess://") {
			encoded := strings.TrimPrefix(line, "vmess://")
			for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
				data, err := encoding.DecodeString(encoded)
				if err != nil {
					continue
				}
				var payload struct {
					Name string `json:"ps"`
				}
				if json.Unmarshal(data, &payload) == nil {
					name = payload.Name
					break
				}
			}
		}
		if name == "" {
			name = fallback
		}
		if name == "" {
			name = strings.SplitN(line, ":", 2)[0]
		}
		*s.botNodeNames = append(*s.botNodeNames, strings.Join(strings.Fields(name), " "))
	}
}
