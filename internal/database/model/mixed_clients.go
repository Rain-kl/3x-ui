package model

import "encoding/json"

func MixedClientsToCore(settings string) (string, bool) {
	var object map[string]any
	if json.Unmarshal([]byte(settings), &object) != nil {
		return settings, false
	}
	clients, ok := object["clients"].([]any)
	if !ok {
		return settings, false
	}
	active := make([]any, 0, len(clients))
	for _, raw := range clients {
		client, ok := raw.(map[string]any)
		if !ok {
			return settings, false
		}
		if client["enable"] == false {
			continue
		}
		name, _ := client["username"].(string)
		if name == "" {
			name, _ = client["user"].(string)
		}
		if name == "" {
			name, _ = client["email"].(string)
		}
		client["user"] = name
		if password, ok := client["password"]; ok {
			client["pass"] = password
		}
		delete(client, "username")
		delete(client, "password")
		active = append(active, client)
	}
	object["clients"] = active
	encoded, err := json.Marshal(object)
	if err != nil {
		return settings, false
	}
	return string(encoded), true
}
