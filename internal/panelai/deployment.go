package panelai

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

const openCodeVersion = "2.0.32"

type AIDeployment struct {
	State   string `json:"state"`
	Phase   string `json:"phase"`
	Error   string `json:"error"`
	Version string `json:"version"`
}

func (m *Manager) Deployment() AIDeployment {
	m.deploymentMu.Lock()
	defer m.deploymentMu.Unlock()
	value := m.deployment
	if value.State == "" {
		value.State = "idle"
	}
	return value
}

func (m *Manager) Deploy() (AIDeployment, error) {
	m.deploymentMu.Lock()
	defer m.deploymentMu.Unlock()
	if m.deployment.State == "running" {
		return m.deployment, nil
	}
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if m.ctx == nil || m.ctx.Err() != nil {
		return AIDeployment{}, fmt.Errorf("AI runtime is not running")
	}
	m.deployment = AIDeployment{State: "running", Phase: "download", Version: openCodeVersion}
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
		defer cancel()
		err := m.install(ctx)
		m.deploymentMu.Lock()
		defer m.deploymentMu.Unlock()
		if err != nil {
			m.deployment.State = "failed"
			m.deployment.Error = err.Error()
		} else {
			m.deployment.State = "completed"
			m.deployment.Phase = "ready"
		}
	}()
	return m.deployment, nil
}

func (m *Manager) installPhase(phase string) {
	m.deploymentMu.Lock()
	m.deployment.Phase = phase
	m.deploymentMu.Unlock()
}

func deploymentAsset() (string, string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		if !muslHost() {
			return "https://registry.npmjs.org/@opencode/cli-linux-x64-baseline/-/cli-linux-x64-baseline-2.0.32.tgz", "NrB4iglJb7vGZWJ6B0qVH1EMHhWBZ4ADsAG9W0xLanzgxJ6eLEEsGBvxRX0cm/KPag5E/N0yZGsDVDZYcnz/OQ==", nil
		}
		return "https://registry.npmjs.org/@opencode/cli-linux-x64-baseline-musl/-/cli-linux-x64-baseline-musl-2.0.32.tgz", "SdZFMhObZVoK+XQYXxv2Hwey/n6+tHzVAEMgICjd/T8n6no9B0T/+5VMFnFsEaqPGIx3GvU3e0AdnoHLp0Uh0w==", nil
	case "linux/arm64":
		if !muslHost() {
			return "https://registry.npmjs.org/@opencode/cli-linux-arm64/-/cli-linux-arm64-2.0.32.tgz", "YR5O03irvmot/G0f8W49ehvhn5uqCFN3vf9JTsChvf47W6ldjKCNvsaCSXS2SP26dPOUMQBn7WqbKnyHzrNR5w==", nil
		}
		return "https://registry.npmjs.org/@opencode/cli-linux-arm64-musl/-/cli-linux-arm64-musl-2.0.32.tgz", "C0FaJbYTeoAmN73bWgSIXIeJcbWXhTQ2c/Df4DSQ8FwDY6bvOuqhsvVz4rTXvGvIqsRtVDfofWp01F4aQlwk5A==", nil
	case "darwin/amd64":
		return "https://registry.npmjs.org/@opencode/cli-darwin-x64-baseline/-/cli-darwin-x64-baseline-2.0.32.tgz", "r/T6QeDkYqzDHdI6VclvlJXqU6j0tbBmsP1+a1S+AyZxqEsY4vm9veFaoT5WQ/ekGH4TsAp3x0XtyF/m8eLFoQ==", nil
	case "darwin/arm64":
		return "https://registry.npmjs.org/@opencode/cli-darwin-arm64/-/cli-darwin-arm64-2.0.32.tgz", "wASwXBsWaOAYcZrh4ZLl4oaVxPCUAGVb7zZvXBBF8TjWE4uWgY0D2tj6gn80JfcO3b/s9woM6dP/gKqYKK2EdQ==", nil
	case "windows/amd64":
		return "https://registry.npmjs.org/@opencode/cli-windows-x64-baseline/-/cli-windows-x64-baseline-2.0.32.tgz", "8vim3HxOdy5VaL686BvwozryJCaiet5PiMIC/Ir9rP6h9m+qtd9KBV/2Hd2yuXvpq8ykNlFgHziAqwfsbkyrQg==", nil
	default:
		return "", "", fmt.Errorf("OpenCode one-click deployment does not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func (m *Manager) install(ctx context.Context) error {
	asset, digest, err := deploymentAsset()
	if err != nil {
		return err
	}
	current, err := readSettings()
	if err != nil {
		return err
	}
	if current.Binary != "" {
		managedRoot, pathErr := filepath.Abs(filepath.Join(config.GetDBFolderPath(), "ai-cli"))
		if pathErr != nil {
			return fmt.Errorf("resolve managed OpenCode directory: %w", pathErr)
		}
		relative, relErr := filepath.Rel(managedRoot, current.Binary)
		if relErr == nil && !strings.HasPrefix(relative, "..") && !filepath.IsAbs(relative) {
			directory, pathErr := filepathAbsolute(workspaceDirectory(current))
			if pathErr != nil {
				return pathErr
			}
			probe := &openCode{binary: current.Binary, directory: directory}
			if err = prepareWorkspace(probe.directory); err != nil {
				return err
			}
			if version, versionErr := probe.health(ctx); versionErr == nil && version == openCodeVersion {
				current.Enabled = true
				return SaveSettings(current)
			}
		}
	}
	root, pathErr := filepath.Abs(filepath.Join(config.GetDBFolderPath(), "ai-cli"))
	if pathErr != nil {
		return fmt.Errorf("resolve OpenCode installation directory: %w", pathErr)
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create OpenCode installation directory: %w", err)
	}
	archive, err := os.CreateTemp(root, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 9 * time.Minute}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download official OpenCode %s: %w", asset, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download official OpenCode: HTTP %d", response.StatusCode)
	}
	hash := sha512.New()
	size, err := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, (512<<20)+1))
	if err != nil {
		return fmt.Errorf("download OpenCode archive: %w", err)
	}
	if size > 512<<20 {
		return fmt.Errorf("OpenCode archive exceeds 512 MiB")
	}
	m.installPhase("verify")
	if base64.StdEncoding.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("OpenCode %s SHA-512 checksum mismatch; installation unchanged", asset)
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	name := "opencode"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	staging, err := os.MkdirTemp(root, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	executable := filepath.Join(staging, name)
	m.installPhase("install")
	if err = extractCLI(archive, asset, executable, name, size); err != nil {
		return err
	}
	if err = os.Chmod(executable, 0o700); err != nil {
		return err
	}
	cfg, err := readSettings()
	if err != nil {
		return err
	}
	directory, err := filepathAbsolute(workspaceDirectory(cfg))
	if err != nil {
		return err
	}
	if err = prepareWorkspace(directory); err != nil {
		return err
	}
	candidate := &openCode{binary: executable, directory: directory}
	version, err := candidate.health(ctx)
	if err != nil {
		return fmt.Errorf("verify installed OpenCode executable: %w", err)
	}
	if version != openCodeVersion {
		return fmt.Errorf("OpenCode version mismatch: got %q, expected %s", version, openCodeVersion)
	}
	// Keep each installation immutable so failed activation never replaces the current executable.
	destination := filepath.Join(root, "v"+openCodeVersion+"-"+randomID())
	if err = os.Rename(staging, destination); err != nil {
		return fmt.Errorf("activate OpenCode installation: %w", err)
	}
	absolute, err := filepath.Abs(filepath.Join(destination, name))
	if err != nil {
		return err
	}
	cfg.Binary, cfg.Directory, cfg.Enabled = absolute, directory, true
	if err = SaveSettings(cfg); err != nil {
		_ = os.RemoveAll(destination)
		return fmt.Errorf("save deployed OpenCode settings: %w", err)
	}
	return nil
}

func extractCLI(archive *os.File, asset, destination, name string, size int64) error {
	var source io.Reader
	var closeSource io.Closer
	if strings.HasSuffix(asset, ".zip") {
		reader, err := zip.NewReader(archive, size)
		if err != nil {
			return fmt.Errorf("open OpenCode ZIP: %w", err)
		}
		for _, entry := range reader.File {
			if entry.Name == name && entry.Mode().IsRegular() {
				stream, err := entry.Open()
				if err != nil {
					return err
				}
				source, closeSource = stream, stream
				break
			}
		}
	} else {
		reader, err := gzip.NewReader(archive)
		if err != nil {
			return fmt.Errorf("open OpenCode gzip: %w", err)
		}
		defer reader.Close()
		tarReader := tar.NewReader(reader)
		for {
			entry, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("read OpenCode tar: %w", err)
			}
			if (strings.TrimPrefix(entry.Name, "./") == name || entry.Name == "package/bin/"+name) && entry.Typeflag == tar.TypeReg {
				source = tarReader
				break
			}
		}
	}
	if closeSource != nil {
		defer closeSource.Close()
	}
	if source == nil {
		return fmt.Errorf("official archive does not contain regular %s executable", name)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	defer file.Close()
	copied, err := io.Copy(file, io.LimitReader(source, (1<<30)+1))
	if err != nil {
		return err
	}
	if copied > 1<<30 {
		return fmt.Errorf("OpenCode executable exceeds 1 GiB")
	}
	return file.Sync()
}

func muslHost() bool {
	if _, err := os.Stat("/etc/alpine-release"); err == nil {
		return true
	}
	matches, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(matches) > 0
}
