package officialweb

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io/fs"
	"sync"
)

//go:embed app.zip
var archive []byte

var load = sync.OnceValues(func() (fs.FS, error) {
	return zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
})

func Files() (fs.FS, error) { return load() }
