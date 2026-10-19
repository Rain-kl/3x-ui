package telegramuser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/telegramformat"
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
	chunks := telegramformat.Chunks(telegramformat.Markdown(text), 3500)
	var failures []error
	for _, chatID := range network.AdminIDs {
		for index, chunk := range chunks {
			err = api.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": chunk, "parse_mode": "HTML"}, nil)
			if err != nil {
				failures = append(failures, fmt.Errorf("send AI report to administrator chat %d: %w", chatID, err))
				break
			}
			if index < len(chunks)-1 {
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
