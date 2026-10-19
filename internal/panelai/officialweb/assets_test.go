package officialweb

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedAppUsesHashRouter(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatalf("open embedded UI: %v", err)
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	entry := regexp.MustCompile(`src="\./(_assets/index-[^"]+\.js)"`).FindSubmatch(index)
	if entry == nil {
		t.Fatal("index.html does not reference an entry script")
	}
	script, err := fs.ReadFile(files, string(entry[1]))
	if err != nil {
		t.Fatalf("read %s: %v", entry[1], err)
	}
	if !strings.Contains(string(script), "hashchange") {
		t.Fatalf("%s does not use the hash router; the path router reads the panel base prefix as a route and fails under a non-root deployment", entry[1])
	}
}
