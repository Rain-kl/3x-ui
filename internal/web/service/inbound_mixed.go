package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func mixedUsername(client model.Client) string {
	if client.Username != "" {
		return client.Username
	}
	return client.Email
}

func validateMixedClient(client model.Client) error {
	name := mixedUsername(client)
	if len(name) == 0 || len(name) > 255 || strings.ContainsAny(name, ":\r\n") {
		return fmt.Errorf("mixed client %q: username must be 1..255 bytes without colon or line breaks", client.Email)
	}
	if len(client.Password) == 0 || len(client.Password) > 255 {
		return fmt.Errorf("mixed client %q: password must be 1..255 bytes", client.Email)
	}
	return nil
}

func normalizeMixedSettings(inbound *model.Inbound, oldSettings string) error {
	if inbound.Protocol != model.Mixed {
		return nil
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return fmt.Errorf("decode mixed settings: %w", err)
	}
	if settings == nil {
		return fmt.Errorf("mixed settings must be an object")
	}
	auth, _ := settings["auth"].(string)
	if auth == "" {
		auth = "password"
		settings["auth"] = auth
	}
	if auth != "password" && auth != "noauth" {
		return fmt.Errorf("mixed auth must be password or noauth")
	}
	_, hasClients := settings["clients"]
	accounts, _ := settings["accounts"].([]any)
	if !hasClients && len(accounts) == 0 && oldSettings != "" {
		var old map[string]any
		if err := json.Unmarshal([]byte(oldSettings), &old); err != nil {
			return fmt.Errorf("decode previous mixed settings: %w", err)
		}
		accounts, _ = old["accounts"].([]any)
	}
	if auth == "noauth" {
		if inbound.ClientDownLimit > 0 || inbound.InboundDownLimit > 0 {
			return fmt.Errorf("anonymous mixed inbounds cannot enforce authenticated client bandwidth limits")
		}
		clients, err := ParseInboundSettingsClients(inbound.Settings)
		if err != nil {
			return err
		}
		if len(clients) > 0 {
			return fmt.Errorf("mixed client management requires password authentication")
		}
		delete(settings, "clients")
		delete(settings, "accounts")
	} else {
		if hasClients && len(accounts) > 0 {
			return fmt.Errorf("mixed clients cannot be combined with legacy accounts")
		}
		if !hasClients {
			clients := make([]model.Client, 0, len(accounts))
			for _, raw := range accounts {
				account, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("mixed account must be an object")
				}
				name, _ := account["user"].(string)
				password, _ := account["pass"].(string)
				key := fmt.Sprintf("%s/%s/%d/%s", inbound.OriginNodeGuid, inbound.Tag, inbound.Id, name)
				digest := sha256.Sum256([]byte(key))
				client := model.Client{Email: fmt.Sprintf("mixed-%x", digest[:12]), Username: name, Password: password, ID: uuid.NewString(), SubID: uuid.NewString(), Enable: true}
				if err := validateMixedClient(client); err != nil {
					return err
				}
				clients = append(clients, client)
			}
			settings["clients"] = clients
		}
		delete(settings, "accounts")
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode normalized mixed settings: %w", err)
	}
	inbound.Settings = string(encoded)
	if auth == "password" {
		clients, err := ParseInboundSettingsClients(inbound.Settings)
		if err != nil {
			return err
		}
		names := make(map[string]bool, len(clients))
		for _, client := range clients {
			if err := validateMixedClient(client); err != nil {
				return err
			}
			name := mixedUsername(client)
			if names[name] {
				return fmt.Errorf("mixed inbound has duplicate username %q", name)
			}
			names[name] = true
		}
	}
	return nil
}

func (s *ClientService) validateMixedIdentities(tx *gorm.DB, inbound *model.Inbound, id int, changed []model.Client, detached []string, prune bool) error {
	var settings struct {
		Auth     string            `json:"auth"`
		Accounts []json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return fmt.Errorf("read mixed authentication: %w", err)
	}
	if len(settings.Accounts) > 0 {
		return fmt.Errorf("save this inbound once to migrate legacy mixed accounts before attaching clients")
	}
	candidates := append([]model.Client(nil), changed...)
	if !prune {
		current, err := s.ListForInbound(tx, id)
		if err != nil {
			return err
		}
		excluded := make(map[string]bool)
		for _, client := range changed {
			excluded[client.Email] = true
		}
		for _, email := range detached {
			excluded[email] = true
		}
		for _, client := range current {
			if !excluded[client.Email] {
				candidates = append(candidates, client)
			}
		}
	}
	if settings.Auth != "password" && len(candidates) > 0 {
		return fmt.Errorf("mixed client management requires password authentication")
	}
	names := make(map[string]string, len(candidates))
	for _, client := range candidates {
		if err := validateMixedClient(client); err != nil {
			return err
		}
		name := mixedUsername(client)
		if email, exists := names[name]; exists && email != client.Email {
			return fmt.Errorf("mixed username %q is already used by another client in this inbound", name)
		}
		names[name] = client.Email
	}
	return nil
}
