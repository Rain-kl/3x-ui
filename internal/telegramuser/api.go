package telegramuser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/mhsanaei/3x-ui/v3/internal/telegramformat"
)

const MaxAttachment = 20 << 20

// FORK(telegram-user-bot): Long polls need connection and TLS headroom beyond Telegram's server-side wait.
const telegramLongPollSeconds = 25

type Network struct {
	Proxy      string
	APIServer  string
	AdminToken string
	AdminIDs   []int64
}

type botAPI struct {
	token   string
	base    string
	client  *http.Client
	secrets []string
}

type apiError struct {
	Code  int
	Text  string
	Retry int
}

func (e *apiError) Error() string { return fmt.Sprintf("Telegram API %d: %s", e.Code, e.Text) }

func newBotAPI(token string, network Network) (*botAPI, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	secrets := []string{token}
	if network.Proxy != "" {
		u, err := url.Parse(network.Proxy)
		if err != nil {
			var parseErr *url.Error
			if errors.As(err, &parseErr) {
				return nil, fmt.Errorf("parse Telegram proxy: %w", parseErr.Err)
			}
			return nil, fmt.Errorf("parse Telegram proxy: %w", err)
		}
		secrets = append(secrets, network.Proxy)
		if u.User != nil {
			if password, ok := u.User.Password(); ok && password != "" {
				secrets = append(secrets, password)
			}
		}
		if u.Scheme == "socks5" || u.Scheme == "socks5h" {
			u.Scheme = "socks5"
			dialer, err := proxy.FromURL(u, &net.Dialer{Timeout: 15 * time.Second})
			if err != nil {
				return nil, fmt.Errorf("create Telegram SOCKS proxy: %w", err)
			}
			contextDialer, ok := dialer.(proxy.ContextDialer)
			if !ok {
				return nil, fmt.Errorf("Telegram proxy does not support context cancellation")
			}
			transport.DialContext = contextDialer.DialContext
		} else {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	base := strings.TrimRight(network.APIServer, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &botAPI{token: token, base: base, client: &http.Client{Transport: transport, Timeout: 40 * time.Second}, secrets: secrets}, nil
}

func (a *botAPI) redactText(text string) string {
	for _, secret := range a.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}

func (a *botAPI) redact(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", a.redactText(err.Error()))
}

func markdownParams(method string, params any) any {
	values, ok := params.(map[string]any)
	if !ok || values["parse_mode"] != nil {
		return params
	}
	if method != "sendMessage" && method != "editMessageText" && !strings.HasPrefix(method, "send") && method != "editMessageCaption" {
		return params
	}
	copy := make(map[string]any, len(values)+1)
	for key, value := range values {
		copy[key] = value
	}
	for _, field := range []string{"text", "caption"} {
		if text, ok := copy[field].(string); ok && text != "" {
			copy[field] = telegramformat.Markdown(text)
			copy["parse_mode"] = "HTML"
		}
	}
	return copy
}

func (a *botAPI) call(ctx context.Context, method string, params any, result any) error {
	params = markdownParams(method, params)
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode Telegram %s: %w", method, err)
	}
	return a.request(ctx, method, "application/json", bytes.NewReader(body), result)
}

func (a *botAPI) request(ctx context.Context, method, contentType string, body io.Reader, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/bot"+a.token+"/"+method, body)
	if err != nil {
		return a.redact(err)
	}
	req.Header.Set("Content-Type", contentType)
	client := a.client
	if method == "getUpdates" {
		pollClient := *a.client
		pollClient.Timeout = (telegramLongPollSeconds + 35) * time.Second
		client = &pollClient
	}
	response, err := client.Do(req)
	if err != nil {
		return a.redact(err)
	}
	defer response.Body.Close()
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode Telegram %s (HTTP %d): %w", method, response.StatusCode, err)
	}
	if !envelope.OK {
		return &apiError{Code: envelope.ErrorCode, Text: a.redactText(envelope.Description), Retry: envelope.Parameters.RetryAfter}
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode Telegram %s result: %w", method, err)
		}
	}
	return nil
}

func (a *botAPI) upload(ctx context.Context, method, field, filename string, data []byte, params map[string]any, result any) error {
	params = markdownParams(method, params).(map[string]any)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for key, value := range params {
		text, ok := value.(string)
		if !ok {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			text = string(raw)
		}
		if err := w.WriteField(key, text); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return a.request(ctx, method, w.FormDataContentType(), &body, result)
}

func (a *botAPI) download(ctx context.Context, fileID string) ([]byte, string, error) {
	var file struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := a.call(ctx, "getFile", map[string]any{"file_id": fileID}, &file); err != nil {
		return nil, "", err
	}
	if file.Size > MaxAttachment || file.Path == "" || strings.Contains(file.Path, "..") || strings.Contains(file.Path, "://") {
		return nil, "", fmt.Errorf("Telegram attachment is unavailable or exceeds 20 MiB")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/file/bot"+a.token+"/"+file.Path, nil)
	if err != nil {
		return nil, "", a.redact(err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, "", a.redact(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download Telegram attachment: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxAttachment+1))
	if err != nil {
		return nil, "", fmt.Errorf("read Telegram attachment: %w", err)
	}
	if len(data) > MaxAttachment {
		return nil, "", fmt.Errorf("Telegram attachment exceeds 20 MiB")
	}
	return data, resp.Header.Get("Content-Type"), nil
}
