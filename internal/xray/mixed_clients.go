package xray

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const MixedClientsMinimumVersion = "v26.10.10-feat.4"

func SupportsMixedClientVersion(value string) bool {
	if !SupportsNativeRateLimitVersion(value) {
		return false
	}
	base, suffix, _ := strings.Cut(strings.TrimPrefix(value, "v"), "-feat.")
	if base != "26.10.10" {
		return true
	}
	n, err := strconv.Atoi(suffix)
	return err == nil && n >= 4
}

func HasManagedMixed(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	for _, ib := range cfg.InboundConfigs {
		if ib.Protocol != "mixed" {
			continue
		}
		var settings map[string]json.RawMessage
		if json.Unmarshal(ib.Settings, &settings) == nil {
			if _, managed := settings["clients"]; managed {
				return true
			}
		}
	}
	return false
}

func ValidateMixedClientVersion(cfg *Config, version string) error {
	if HasManagedMixed(cfg) && !SupportsMixedClientVersion(version) {
		return fmt.Errorf("mixed clients require Rain-kl/Xray-core %s or newer; current core: %s", MixedClientsMinimumVersion, version)
	}
	return nil
}
