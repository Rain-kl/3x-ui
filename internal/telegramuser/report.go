package telegramuser

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SendAdminReport reuses the administrator transport without a second update receiver.
func SendAdminReport(ctx context.Context, network Network, text string) error {
	if network.AdminToken == "" || len(network.AdminIDs) == 0 {
		return fmt.Errorf("administrator Bot token and chat IDs are required")
	}
	api, err := newBotAPI(network.AdminToken, network)
	if err != nil {
		return err
	}
	runes := []rune(text)
	var failures []error
	for _, chatID := range network.AdminIDs {
		for offset := 0; offset < len(runes); offset += 3500 {
			end := min(offset+3500, len(runes))
			err = api.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": string(runes[offset:end])}, nil)
			if err != nil {
				failures = append(failures, fmt.Errorf("send AI report to administrator chat %d: %w", chatID, err))
				break
			}
			if end < len(runes) {
				timer := time.NewTimer(350 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	return errors.Join(failures...)
}
