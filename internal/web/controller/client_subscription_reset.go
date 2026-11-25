package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *ClientController) resetSubscription(c *gin.Context) {
	email, ok := clientEmailParam(c)
	if !ok {
		return
	}
	action := I18nWeb(c, "pages.clients.resetSubscription")
	record, err := a.clientService.GetRecordByEmail(nil, email)
	if err != nil {
		jsonMsg(c, action, fmt.Errorf("load subscription client: %w", err))
		return
	}
	// Sequential node confirmations can outlast the normal HTTP write deadline.
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		jsonMsg(c, action, fmt.Errorf("extend subscription reset response deadline: %w", err))
		return
	}
	_, err = a.clientService.ResetSubscriptionCredentials(record.Id)
	// Even an unsuccessful reset can have persisted a new enabled state or credentials.
	notifyClientsChanged()
	if err != nil {
		jsonMsg(c, action, err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.clients.toasts.subscriptionReset"), nil)
}
