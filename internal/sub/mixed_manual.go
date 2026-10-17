package sub

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type SubMixedNode struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	HTTP     string `json:"http"`
	SOCKS5   string `json:"socks5"`
}

func (s *SubService) manualMixedNodes(subID string) ([]SubMixedNode, error) {
	type row struct {
		model.Inbound
		Username string
		Password string
		Email    string
	}
	var rows []row
	err := database.GetDB().Table("inbounds").
		Select("inbounds.*, clients.username, clients.password, clients.email").
		Joins("JOIN client_inbounds ON client_inbounds.inbound_id = inbounds.id").
		Joins("JOIN clients ON clients.id = client_inbounds.client_id").
		Joins("LEFT JOIN client_traffics ON client_traffics.email = clients.email").
		Where("inbounds.protocol = ? AND inbounds.enable = ? AND clients.sub_id = ? AND clients.enable = ?", model.Mixed, true, subID, true).
		Where("clients.expiry_time <= 0 OR clients.expiry_time > ?", time.Now().UnixMilli()).
		Where("clients.total_gb <= 0 OR COALESCE(client_traffics.up, 0) + COALESCE(client_traffics.down, 0) < clients.total_gb").
		Where("client_inbounds.total_gb <= 0 OR client_inbounds.up + client_inbounds.down < client_inbounds.total_gb").
		Order("inbounds.sub_sort_index, inbounds.id, clients.id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load subscription mixed nodes: %w", err)
	}
	result := make([]SubMixedNode, 0, len(rows))
	for _, r := range rows {
		var settings struct {
			Auth string `json:"auth"`
		}
		if err := json.Unmarshal([]byte(r.Settings), &settings); err != nil {
			return nil, fmt.Errorf("mixed inbound %d settings: %w", r.Id, err)
		}
		if settings.Auth != "password" || r.Password == "" {
			continue
		}
		name := r.Username
		if name == "" {
			name = r.Email
		}
		address := strings.Trim(s.resolveInboundAddress(&r.Inbound), "[]")
		title := r.Remark
		if title == "" {
			title = r.Tag
		}
		endpoint := net.JoinHostPort(address, strconv.Itoa(r.Port))
		makeURL := func(scheme string) string {
			return (&url.URL{Scheme: scheme, Host: endpoint, User: url.UserPassword(name, r.Password)}).String()
		}
		result = append(result, SubMixedNode{Name: title, Address: address, Port: r.Port, Username: name, Password: r.Password, HTTP: makeURL("http"), SOCKS5: makeURL("socks5")})
	}
	return result, nil
}
