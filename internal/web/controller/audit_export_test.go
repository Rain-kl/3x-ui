package controller

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

func TestAuditExportNativeDownload(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	engine := gin.New()
	newAuditController(engine.Group("/audit"))
	server := httptest.NewServer(engine)
	defer server.Close()
	response, err := http.Get(server.URL + "/audit/export?kind=database")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("download status=%d headers=%v body=%s", response.StatusCode, response.Header, data)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 10 {
		t.Fatalf("archive files=%d, want 10", len(archive.File))
	}
}
