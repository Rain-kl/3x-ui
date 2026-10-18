package telegramuser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type Hooks struct {
	Network      func() (Network, error)
	Subscription func(int, bool) (string, error)
	Usage        func(int) (string, error)
}

type UserTelegramStatus struct {
	Running     bool   `json:"running"`
	BotUsername string `json:"botUsername"`
	LastError   string `json:"lastError"`
}

type Manager struct {
	mu      sync.RWMutex
	reload  sync.Mutex
	hooks   Hooks
	parent  context.Context
	current *receiver
	lastErr string
}

type awaiting struct {
	action   string
	clientID int
	until    time.Time
}

type receiver struct {
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	cfg     model.TelegramUserConfig
	network Network
	hooks   Hooks
	user    *botAPI
	admin   *botAPI
	await   map[int64]awaiting

	// FORK(telegram-custom-commands): Keep the native slash menu current without restarting polling.
	commandMenu string
}

var Default = &Manager{}

func (m *Manager) Start(ctx context.Context, hooks Hooks) error {
	m.mu.Lock()
	m.parent, m.hooks = ctx, hooks
	m.mu.Unlock()
	return m.Reload()
}

func (m *Manager) Stop() {
	m.reload.Lock()
	defer m.reload.Unlock()
	m.stop()
}

func (m *Manager) stop() {
	m.mu.Lock()
	r := m.current
	m.current = nil
	m.mu.Unlock()
	if r != nil {
		r.cancel()
		r.wg.Wait()
	}
}

func (m *Manager) configuration() (context.Context, Hooks) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.parent, m.hooks
}

func (m *Manager) Reload() (resultErr error) {
	m.reload.Lock()
	defer m.reload.Unlock()
	defer func() {
		if resultErr != nil {
			m.mu.Lock()
			m.lastErr = resultErr.Error()
			m.mu.Unlock()
			if database.GetDB() != nil {
				if err := RecordLog(0, 0, "bot.reload.failed", resultErr.Error()); err != nil {
					logger.Warning("persist user Bot reload failure: ", err)
				}
			}
		}
	}()
	m.stop()
	parent, hooks := m.configuration()
	if parent == nil || hooks.Network == nil {
		return nil
	}
	cfg, err := Config()
	if err != nil || !cfg.Enabled {
		return err
	}
	network, err := hooks.Network()
	if err != nil {
		return err
	}
	if cfg.Token == "" || cfg.Token == network.AdminToken {
		return fmt.Errorf("user Telegram Bot requires its own independent token")
	}
	api, err := newBotAPI(cfg.Token, network)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	if err := database.GetDB().Model(&model.TelegramMessage{}).Where("status = ?", "sending").Updates(map[string]any{"status": "failed", "error": "发送进程中断，无法确认是否送达，请检查后重试"}).Error; err != nil {
		cancel()
		return fmt.Errorf("recover interrupted Telegram sends: %w", err)
	}
	r := &receiver{ctx: ctx, cancel: cancel, cfg: cfg, network: network, hooks: hooks, user: api, await: make(map[int64]awaiting)}
	if network.AdminToken != "" {
		r.admin, err = newBotAPI(network.AdminToken, network)
		if err != nil {
			cancel()
			return err
		}
	}
	m.mu.Lock()
	m.current, m.lastErr = r, ""
	r.wg.Add(3)
	m.mu.Unlock()
	go r.run("poll", r.poll)
	go r.run("delivery", r.deliver)
	go r.run("retention", r.maintain)
	return nil
}

func (r *receiver) run(name string, work func()) {
	defer r.wg.Done()
	defer func() {
		if value := recover(); value != nil {
			r.failure("bot.worker.failed", fmt.Errorf("user Telegram %s worker panic: %v", name, value))
			r.cancel()
		}
	}()
	work()
}

func (m *Manager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current != nil && m.current.ctx.Err() == nil
}

func (m *Manager) Status() UserTelegramStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := UserTelegramStatus{Running: m.current != nil && m.current.ctx.Err() == nil, LastError: m.lastErr}
	if m.current != nil {
		result.BotUsername = m.current.cfg.BotUsername
	}
	return result
}

func (m *Manager) receiver() (*receiver, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil || m.current.ctx.Err() != nil {
		return nil, fmt.Errorf("user Telegram Bot is not running")
	}
	return m.current, nil
}

func (r *receiver) log(tgID, messageID int64, event, detail string) {
	if err := RecordLog(tgID, messageID, event, detail); err != nil {
		logger.Warningf("user Telegram log persistence (%s): %v", event, err)
	}
}

func (r *receiver) failure(event string, err error) {
	if r.ctx.Err() != nil {
		return
	}
	Default.mu.Lock()
	Default.lastErr = err.Error()
	Default.mu.Unlock()
	r.log(0, 0, event, err.Error())
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *receiver) poll() {
	var me telego.User
	if err := r.user.call(r.ctx, "getMe", map[string]any{}, &me); err != nil {
		r.failure("bot.start.failed", err)
	} else {
		Default.mu.Lock()
		r.cfg.BotUsername = me.Username
		Default.mu.Unlock()
		if err := database.GetDB().Model(&model.TelegramUserConfig{}).Where("id = ? AND token_hash = ?", 1, r.cfg.TokenHash).Update("bot_username", me.Username).Error; err != nil {
			r.failure("bot.state.failed", err)
		}
	}
	offset := r.cfg.PollOffset
	backoff := 5 * time.Second
	for r.ctx.Err() == nil {
		// FORK(telegram-custom-commands): Retry changed slash menus after long polls.
		r.syncCommands()
		var updates []telego.Update
		err := r.user.call(r.ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "edited_message", "callback_query"}}, &updates)
		if err != nil {
			r.failure("bot.poll.failed", err)
			if !pause(r.ctx, backoff) {
				return
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = 5 * time.Second
		for _, update := range updates {
			if err := r.handle(update); err != nil {
				r.failure("bot.update.failed", err)
				pause(r.ctx, 2*time.Second)
				break
			}
			offset = int64(update.UpdateID) + 1
			if err := database.GetDB().Model(&model.TelegramUserConfig{}).Where("id = ? AND token_hash = ?", 1, r.cfg.TokenHash).Update("poll_offset", offset).Error; err != nil {
				r.failure("bot.offset.failed", err)
				break
			}
		}
	}
}

func (r *receiver) handle(update telego.Update) error {
	message := update.Message
	if message == nil {
		message = update.EditedMessage
	}
	if message != nil {
		if message.From == nil || message.Chat.Type != "private" || message.Chat.ID != message.From.ID {
			r.log(message.Chat.ID, 0, "message.ignored", "user Bot accepts private chats only")
			return nil
		}
		if r.dropBlocked(message.From.ID) {
			return nil
		}
		return r.receive(message, update.UpdateID, update.EditedMessage != nil)
	}
	if query := update.CallbackQuery; query != nil {
		if query.Message == nil || query.Message.GetChat().Type != "private" || query.Message.GetChat().ID != query.From.ID {
			return nil
		}
		if r.dropBlocked(query.From.ID) {
			return nil
		}
		if err := r.user.call(r.ctx, "answerCallbackQuery", map[string]any{"callback_query_id": query.ID}, nil); err != nil {
			r.log(query.From.ID, 0, "callback.answer.failed", err.Error())
		}
		r.log(query.From.ID, 0, "command.callback", query.Data)
		r.confirm(query.From.ID, query.Data)
	}
	return nil
}

// dropBlocked silently discards updates from blacklisted accounts; errors fail open.
func (r *receiver) dropBlocked(tgID int64) bool {
	blocked, err := userBlocked(tgID)
	if err != nil {
		r.log(tgID, 0, "blacklist.check.failed", err.Error())
		return false
	}
	return blocked
}

func (r *receiver) receive(incoming *telego.Message, updateID int, edited bool) error {
	tgID := incoming.From.ID
	text := strings.TrimSpace(incoming.Text)
	state, waiting := r.await[tgID]
	if waiting && time.Now().After(state.until) {
		delete(r.await, tgID)
		waiting = false
	}
	client, boundErr := BoundClient(tgID)
	isCommand := !edited && strings.HasPrefix(text, "/")
	isProof := !edited && !isCommand && (strings.HasPrefix(text, "bind_") || (waiting && state.action == "bind"))
	if !isProof && !edited && !isCommand && boundErr != nil && text != "" {
		matched, matchErr := hasSubID(text)
		if matchErr != nil {
			return fmt.Errorf("check Telegram binding credential: %w", matchErr)
		}
		isProof = matched
	}
	row := messageFromTelegram(incoming)
	row.TgID, row.TargetChatID, row.SourceBot, row.DeliveryBot, row.Direction, row.Status = tgID, tgID, "user", "user", "incoming", "received"
	row.Role = "chat"
	if boundErr == nil {
		row.ClientID = client.Id
	}
	key := fmt.Sprintf("user:%s:%d", codeHash(r.cfg.Token)[:12], updateID)
	row.DedupKey = &key
	if isCommand || isProof {
		row.Role = "command"
	}
	if edited && (strings.HasPrefix(text, "bind_") || strings.HasPrefix(text, "/bind ") || strings.HasPrefix(text, "/start ") || (r.cfg.PublicSubURL != "" && strings.HasPrefix(text, r.cfg.PublicSubURL))) {
		row.Role = "command"
	}
	chatAllowed := r.cfg.AllowAnonymousChat || boundErr == nil
	if !isCommand && !isProof && !chatAllowed {
		row.Role = "restricted"
	}
	if edited {
		row.Text = "[编辑] " + row.Text
	}
	inserted, err := recordMessage(&row, incoming.From.Username, strings.TrimSpace(incoming.From.FirstName+" "+incoming.From.LastName))
	if err != nil || !inserted {
		return err
	}
	if isProof {
		delete(r.await, tgID)
		r.bind(tgID, text)
		return nil
	}
	if isCommand {
		r.command(tgID, text)
		return nil
	}
	if !chatAllowed {
		r.log(tgID, row.Id, "message.chat.restricted", "anonymous chat disabled; valid client binding required")
		return nil
	}
	if r.cfg.ForwardAdmins {
		if err := r.forward(&row); err != nil {
			r.log(tgID, row.Id, "message.relay.failed", err.Error())
		}
	}
	return nil
}

func (r *receiver) command(tgID int64, text string) {
	delete(r.await, tgID)
	parts := strings.Fields(text)
	command := strings.ToLower(strings.Split(parts[0], "@")[0])
	if len(r.await) > 10000 {
		for id, state := range r.await {
			if time.Now().After(state.until) {
				delete(r.await, id)
			}
		}
	}
	switch command {
	case "/start", "/help":
		if len(parts) == 2 && strings.HasPrefix(parts[1], "bind_") {
			r.bind(tgID, parts[1])
			return
		}
		r.reply(tgID, "欢迎使用订阅助手。发送订阅链接或绑定码进行绑定；绑定后可查询订阅和剩余用量。输入 / 查看可用指令。", nil)
	case "/bind":
		if len(parts) > 1 {
			r.bind(tgID, parts[1])
		} else {
			r.await[tgID] = awaiting{action: "bind", until: time.Now().Add(5 * time.Minute)}
			r.reply(tgID, "请发送管理员提供的完整订阅链接或一次性绑定码。", nil)
		}
	case "/unbind":
		var binding model.TelegramBinding
		if err := database.GetDB().Where("tg_id = ?", tgID).First(&binding).Error; err != nil {
			r.reply(tgID, "当前没有可解除的绑定。", nil)
			return
		}
		r.await[tgID] = awaiting{action: "unbind", clientID: binding.ClientID, until: time.Now().Add(3 * time.Minute)}
		r.reply(tgID, "确认解除绑定？", map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "确认", "callback_data": "unbind"}, {"text": "取消", "callback_data": "cancel"}}}})
	case "/notifications":
		if _, err := BoundClient(tgID); err != nil {
			r.reply(tgID, err.Error(), nil)
			return
		}
		count, err := QueueNotifications(tgID)
		if err != nil {
			r.reply(tgID, "获取通知失败，请稍后重试或联系管理员。", nil)
			r.log(tgID, 0, "notification.request.failed", err.Error())
		} else if count == 0 {
			r.reply(tgID, "当前没有可接收的已发布通知。", nil)
		} else {
			r.reply(tgID, fmt.Sprintf("已将 %d 条通知加入发送队列，请稍候。", count), nil)
		}
	case "/subscription", "/usage", "/reset":
		client, err := BoundClient(tgID)
		if err != nil {
			r.reply(tgID, err.Error(), nil)
			return
		}
		switch command {
		case "/subscription":
			link, err := r.hooks.Subscription(client.Id, false)
			if err != nil {
				r.reply(tgID, "获取订阅失败，请联系管理员。", nil)
				r.log(tgID, 0, "subscription.failed", err.Error())
			} else {
				r.reply(tgID, subscriptionLinkMessage(link), nil)
			}
		case "/usage":
			usage, err := r.hooks.Usage(client.Id)
			if err != nil {
				r.reply(tgID, "查询用量失败，请联系管理员。", nil)
				r.log(tgID, 0, "usage.failed", err.Error())
			} else {
				r.reply(tgID, usage, nil)
			}
		default:
			action := strings.TrimPrefix(command, "/")
			r.await[tgID] = awaiting{action: action, clientID: client.Id, until: time.Now().Add(3 * time.Minute)}
			label := "确认重置订阅链接（旧链接将失效）"
			r.reply(tgID, label+"？", map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "确认", "callback_data": action}, {"text": "取消", "callback_data": "cancel"}}}})
		}
	default:
		// FORK(telegram-custom-commands): Static custom replies share built-in command logging.
		if r.customCommand(tgID, command) {
			return
		}
		r.reply(tgID, "支持 /subscription、/reset、/usage、/notifications、/bind、/unbind。管理员允许匿名聊天时，或完成绑定后，可发送文字或附件联系管理员。", nil)
	}
}

func (r *receiver) bind(tgID int64, proof string) {
	allowed, attempts := allowBindAttempt(tgID)
	if !allowed {
		reason := fmt.Sprintf("bind attempts exceeded: %d in %s", attempts, bindAttemptWindow)
		if err := blockUser(tgID, attempts, reason); err != nil {
			r.log(tgID, 0, "binding.blacklist.failed", err.Error())
		} else {
			r.log(tgID, 0, "binding.blacklisted", reason)
		}
		r.reply(tgID, "绑定尝试次数过多，已限制使用。请联系管理员解除。", nil)
		return
	}
	binding, err := Bind(tgID, proof)
	if err != nil {
		r.reply(tgID, "绑定失败："+err.Error(), nil)
		r.log(tgID, 0, "binding.failed", err.Error())
		return
	}
	r.log(tgID, 0, "binding.created", fmt.Sprintf("client=%d", binding.ClientID))
	r.reply(tgID, "绑定成功。输入 / 选择指令，查询订阅和用量。", nil)
}

func (r *receiver) confirm(tgID int64, action string) {
	state, ok := r.await[tgID]
	delete(r.await, tgID)
	if action == "cancel" {
		r.reply(tgID, "已取消。", nil)
		return
	}
	if ok && action == "unbind" && state.action == action && time.Now().Before(state.until) {
		var binding model.TelegramBinding
		if err := database.GetDB().Where("tg_id = ? AND client_id = ?", tgID, state.clientID).First(&binding).Error; err != nil {
			r.reply(tgID, "绑定已改变，请重新发送 /unbind。", nil)
			return
		}
		if err := Unbind(tgID); err != nil {
			r.reply(tgID, "解绑失败，请联系管理员。", nil)
			r.log(tgID, 0, "binding.remove.failed", err.Error())
			return
		}
		r.log(tgID, 0, "binding.removed", fmt.Sprintf("client=%d", state.clientID))
		r.reply(tgID, "已解绑。", nil)
		return
	}
	client, err := BoundClient(tgID)
	if !ok || time.Now().After(state.until) || state.action != action || err != nil || client.Id != state.clientID {
		r.reply(tgID, "确认已过期或绑定已改变，请重新发送指令。", nil)
		return
	}
	if action == "reset" {
		link, err := r.hooks.Subscription(client.Id, true)
		if err != nil {
			r.reply(tgID, "重置失败，请联系管理员。", nil)
			r.log(tgID, 0, "subscription.reset.failed", err.Error())
			return
		}
		r.log(tgID, 0, "subscription.reset", fmt.Sprintf("client=%d", client.Id))
		r.reply(tgID, "新订阅链接：\n"+subscriptionLinkMessage(link), nil)
	}
}

// FORK(telegram-user-bot): Explain that Clash needs a subscription converter.
func subscriptionLinkMessage(link string) string {
	return link + "\n\nClash 用户需搭配订阅转换工具使用：\nhttps://sublink.osur.eu/"
}

func (r *receiver) reply(tgID int64, text string, keyboard map[string]any) {
	row := model.TelegramMessage{TgID: tgID, TargetChatID: tgID, Direction: "outgoing", Kind: "text", Text: text, Role: "command", Status: "sending", SourceBot: "user", DeliveryBot: "user"}
	if _, err := recordMessage(&row, "", ""); err != nil {
		r.failure("message.persist.failed", err)
		return
	}
	params := map[string]any{"chat_id": tgID, "text": text, "reply_markup": map[string]any{"remove_keyboard": true}}
	if keyboard != nil {
		params["reply_markup"] = keyboard
	}
	var sent telego.Message
	err := r.user.call(r.ctx, "sendMessage", params, &sent)
	r.complete(&row, sent, err, false)
}

func (r *receiver) maintain() {
	for r.ctx.Err() == nil {
		if err := Clear("logs", r.cfg.LogDays); err != nil {
			r.failure("cleanup.failed", err)
		}
		if err := Clear("messages", r.cfg.MessageDays); err != nil {
			r.failure("cleanup.failed", err)
		}
		if err := database.GetDB().Where("expires_at < ?", time.Now().UnixMilli()).Delete(&model.TelegramBindCode{}).Error; err != nil {
			r.failure("cleanup.failed", err)
		}
		sweepBindAttempts()
		if !pause(r.ctx, time.Hour) {
			return
		}
	}
}

func isRetryable(err error) (bool, time.Duration) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		if apiErr.Code == 429 {
			return true, time.Duration(max(apiErr.Retry, 1)) * time.Second
		}
		return apiErr.Code >= 500, 5 * time.Second
	}
	return true, 5 * time.Second
}
