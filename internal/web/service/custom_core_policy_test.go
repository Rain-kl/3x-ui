package service

import (
	"strings"
	"testing"
)

func TestOfficialCoreUpdateIsRejectedBeforeDownload(t *testing.T) {
	initTrafficTestDB(t)
	err := (&ServerService{}).UpdateXray("v26.6.27")
	if err == nil || !strings.Contains(err.Error(), "only the latest Rain-kl/Xray-core feat release") {
		t.Fatalf("official update was not rejected at the policy boundary: %v", err)
	}
	_, err = (&ServerService{}).downloadXRay("v26.6.27")
	if err == nil || !strings.Contains(err.Error(), "only Rain-kl/Xray-core feat releases") {
		t.Fatalf("direct download bypassed the custom-core policy: %v", err)
	}
}
