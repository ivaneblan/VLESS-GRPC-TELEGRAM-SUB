package botapp

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

type App struct {
	svc           *Service
	cfg           *config.Config
	panelMu       sync.RWMutex
	panelByUser   map[int64]int
	pendingMu     sync.RWMutex
	pendingAction map[int64]string
}

func NewApp(cfg *config.Config, sec *config.Secrets, paths config.Paths) *App {
	return &App{
		svc:           NewService(cfg, sec, paths),
		cfg:           cfg,
		panelByUser:   map[int64]int{},
		pendingAction: map[int64]string{},
	}
}

func (a *App) IsApprover(userID int64) bool {
	return a.cfg.Bot.ApproverUserID != 0 && userID == a.cfg.Bot.ApproverUserID
}

func (a *App) setPanel(userID int64, messageID int) {
	a.panelMu.Lock()
	a.panelByUser[userID] = messageID
	a.panelMu.Unlock()
}

func (a *App) getPanel(userID int64) (int, bool) {
	a.panelMu.RLock()
	defer a.panelMu.RUnlock()
	id, ok := a.panelByUser[userID]
	return id, ok
}

func (a *App) send(ctx context.Context, b *bot.Bot, chatID int64, text string, markup *models.InlineKeyboardMarkup) error {
	params := &bot.SendMessageParams{ChatID: chatID, Text: text}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, err := b.SendMessage(ctx, params)
	return err
}

// sendSubscription uses legacy Markdown (v1) for ``` code blocks around vless:// URLs.
// go-telegram's ParseModeMarkdown is MarkdownV2 and breaks on unescaped URL characters.
func (a *App) sendSubscription(ctx context.Context, b *bot.Bot, chatID int64, text string, markup *models.InlineKeyboardMarkup) error {
	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeMarkdownV1,
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, err := b.SendMessage(ctx, params)
	if err == nil {
		return nil
	}
	log.Printf("subscription markdown send failed chat=%d: %v; retrying plain text", chatID, err)
	params.ParseMode = ""
	_, err = b.SendMessage(ctx, params)
	if err != nil {
		log.Printf("subscription plain send failed chat=%d: %v", chatID, err)
	}
	return err
}

func (a *App) sendMD(ctx context.Context, b *bot.Bot, chatID int64, text string, markup *models.InlineKeyboardMarkup) {
	_ = a.sendSubscription(ctx, b, chatID, text, markup)
}

func (a *App) reply(ctx context.Context, b *bot.Bot, msg *models.Message, text string, markup *models.InlineKeyboardMarkup) {
	if msg == nil {
		return
	}
	a.send(ctx, b, msg.Chat.ID, text, markup)
}

func (a *App) replyMD(ctx context.Context, b *bot.Bot, msg *models.Message, text string, markup *models.InlineKeyboardMarkup) {
	if msg == nil {
		return
	}
	a.sendMD(ctx, b, msg.Chat.ID, text, markup)
}

func (a *App) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		a.handleCallback(ctx, b, update)
		return
	}
	if update.Message == nil {
		return
	}
	text := strings.TrimSpace(update.Message.Text)
	if text != "" && text[0] == '/' {
		a.handleCommand(ctx, b, update)
		return
	}
	if text != "" {
		a.handleText(ctx, b, update)
	}
}

func (a *App) handleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	cmd := strings.Fields(msg.Text)[0]
	switch cmd {
	case "/start":
		a.cmdStart(ctx, b, update)
	case "/admin":
		a.cmdAdmin(ctx, b, update)
	case "/subscribe":
		a.cmdSubscribe(ctx, b, update)
	case "/get":
		a.cmdGet(ctx, b, update)
	case "/add_user":
		a.cmdAddUser(ctx, b, update)
	case "/pending":
		a.cmdPending(ctx, b, update)
	case "/users":
		a.cmdUsers(ctx, b, update)
	case "/servers":
		a.cmdServers(ctx, b, update)
	case "/health":
		a.cmdHealth(ctx, b, update)
	case "/traffic":
		a.cmdTraffic(ctx, b, update)
	case "/sync_user":
		a.cmdSyncUser(ctx, b, update)
	case "/revoke":
		a.cmdRevoke(ctx, b, update)
	case "/send_user":
		a.cmdSendUser(ctx, b, update)
	case "/renew_user":
		a.cmdRenewUser(ctx, b, update)
	case "/never_expires":
		a.cmdNeverExpires(ctx, b, update)
	default:
		a.reply(ctx, b, msg, "Используй кнопки или команды /subscribe и /get.", nil)
	}
}

func (a *App) cmdStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	user := msg.From
	if user == nil {
		return
	}
	if a.IsApprover(user.ID) {
		_ = a.svc.WithState(func(st *config.State) error {
			chatID := msg.Chat.ID
			st.ApproverChatID = &chatID
			return nil
		})
		a.reply(ctx, b, msg, "Админ-чат активен. Управление через меню ниже.", AdminMenuKeyboard())
		return
	}
	sent, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text: fmt.Sprintf(
			"Привет. Нажми кнопку ниже, чтобы отправить заявку на получение подписки.\nВыдача идет только после подтверждения от %s.",
			a.svc.ApproverLabel(),
		),
		ReplyMarkup: UserKeyboard(),
	})
	if sent != nil {
		a.setPanel(user.ID, sent.ID)
	}
}

func (a *App) cmdAdmin(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда /admin только для подтверждающего пользователя.", nil)
		return
	}
	a.reply(ctx, b, msg, "Панель управления:", AdminMenuKeyboard())
}

func (a *App) cmdSubscribe(ctx context.Context, b *bot.Bot, update *models.Update) {
	a.createSubscriptionRequest(ctx, b, update.Message)
}

func (a *App) createSubscriptionRequest(ctx context.Context, b *bot.Bot, msg *models.Message) {
	user := msg.From
	if user == nil {
		return
	}
	now := time.Now().Unix()
	requestID := fmt.Sprintf("%d-%d", user.ID, now)
	req := config.Request{
		RequestID: requestID,
		UserID:    user.ID,
		Username:  user.Username,
		FirstName: user.FirstName,
		Status:    "pending",
		CreatedAt: now,
	}
	var approverChatID *int64
	if err := a.svc.WithState(func(st *config.State) error {
		st.Requests[requestID] = req
		approverChatID = st.ApproverChatID
		return nil
	}); err != nil {
		a.reply(ctx, b, msg, "Не удалось сохранить заявку: "+err.Error(), nil)
		return
	}
	if approverChatID == nil {
		a.reply(ctx, b, msg, fmt.Sprintf(
			"Заявка создана, но %s еще не открыл чат с ботом.\nПопроси %s написать боту /start.",
			a.svc.ApproverLabel(), a.svc.ApproverLabel(),
		), nil)
		return
	}
	username := "(без username)"
	if req.Username != "" {
		username = "@" + req.Username
	}
	text := fmt.Sprintf(
		"Новая заявка на подписку\nID: %s\nUser: %s\nUsername: %s\nUser ID: %d",
		req.RequestID, req.FirstName, username, req.UserID,
	)
	a.send(ctx, b, *approverChatID, text, ApproveRejectKeyboard(requestID))
	a.reply(ctx, b, msg, "Заявка отправлена на подтверждение.", UserKeyboard())
}

func (a *App) cmdGet(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда /get доступна только подтверждающему пользователю.", nil)
		return
	}
	label := msg.From.Username
	if label == "" {
		label = "approver"
	}
	userKey := strconv.FormatInt(msg.From.ID, 10)
	var (
		links map[string]map[string]string
		entry config.UserEntry
	)
	if err := a.svc.WithState(func(st *config.State) error {
		a.svc.SweepExpiredUsers(st, 0)
		lm, err := a.svc.GetOrCreateUserLink(st, userKey, label+"-happ")
		if err != nil {
			return err
		}
		links = lm
		e := st.Users[userKey]
		if a.IsApprover(msg.From.ID) {
			e.NeverExpires = true
			e.ExpiresAt = nil
		}
		a.svc.EnsureUserExpiry(&e)
		st.Users[userKey] = e
		entry = e
		return nil
	}); err != nil {
		a.reply(ctx, b, msg, "Ошибка: "+err.Error(), nil)
		return
	}
	a.replyMD(ctx, b, msg, a.svc.BuildHappCodeMessage(links, a.svc.ExpiresAtInt(&entry)), nil)
}

func (a *App) cmdAddUser(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		a.reply(ctx, b, msg, "Использование: /add_user <user_id> [days|never] [label]", nil)
		return
	}
	a.createUserByInput(ctx, b, msg, strings.Join(args[1:], " "))
}

func (a *App) cmdPending(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	a.sendPendingRequests(ctx, b, msg.Chat.ID)
}

func (a *App) cmdUsers(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	text, err := a.FormatUsersMessage()
	if err != nil {
		a.reply(ctx, b, msg, err.Error(), nil)
		return
	}
	a.reply(ctx, b, msg, text, AdminMenuKeyboard())
}

func (a *App) cmdServers(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	a.reply(ctx, b, msg, a.FormatServersMessage(), AdminMenuKeyboard())
}

func (a *App) cmdHealth(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	text, err := a.FormatHealthMessage()
	if err != nil {
		a.reply(ctx, b, msg, err.Error(), nil)
		return
	}
	a.reply(ctx, b, msg, text, AdminMenuKeyboard())
}

func (a *App) cmdTraffic(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	a.reply(ctx, b, msg, a.FormatTrafficMessage(), AdminMenuKeyboard())
}

func (a *App) cmdSyncUser(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		a.reply(ctx, b, msg, "Использование: /sync_user <user_id>", nil)
		return
	}
	if !isDigits(args[1]) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	a.syncUserByID(ctx, b, msg, args[1])
}

func (a *App) cmdRevoke(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		a.reply(ctx, b, msg, "Использование: /revoke <user_id>", nil)
		return
	}
	if !isDigits(args[1]) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	a.revokeUserByID(ctx, b, msg, args[1])
}

func (a *App) cmdSendUser(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		a.reply(ctx, b, msg, "Использование: /send_user <user_id>", nil)
		return
	}
	if !isDigits(args[1]) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	a.sendUserSubscriptionByID(ctx, b, msg, args[1])
}

func (a *App) cmdRenewUser(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 3 {
		a.reply(ctx, b, msg, "Использование: /renew_user <user_id> <days>", nil)
		return
	}
	if !isDigits(args[1]) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	days, err := strconv.Atoi(args[2])
	if err != nil || days <= 0 {
		a.reply(ctx, b, msg, "days должно быть положительным числом.", nil)
		return
	}
	a.renewUserByID(ctx, b, msg, args[1], days)
}

func (a *App) cmdNeverExpires(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if !a.IsApprover(msg.From.ID) {
		a.reply(ctx, b, msg, "Команда только для подтверждающего пользователя.", nil)
		return
	}
	args := strings.Fields(msg.Text)
	if len(args) < 3 {
		a.reply(ctx, b, msg, "Использование: /never_expires <user_id> on|off", nil)
		return
	}
	if !isDigits(args[1]) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	flag := strings.ToLower(args[2])
	var value bool
	switch flag {
	case "on", "1", "true":
		value = true
	case "off", "0", "false":
		value = false
	default:
		a.reply(ctx, b, msg, "Второй аргумент: on или off.", nil)
		return
	}
	a.neverExpiresByID(ctx, b, msg, args[1], value)
}

func (a *App) handleText(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	text := strings.TrimSpace(msg.Text)
	if msg.From != nil {
		a.pendingMu.RLock()
		action := a.pendingAction[msg.From.ID]
		a.pendingMu.RUnlock()
		if action != "" {
			if action == "create_user" {
				a.pendingMu.Lock()
				delete(a.pendingAction, msg.From.ID)
				a.pendingMu.Unlock()
				a.createUserByInput(ctx, b, msg, text)
				return
			}
			if !isDigits(text) {
				a.reply(ctx, b, msg, "Ожидаю числовой user_id.", nil)
				return
			}
			a.pendingMu.Lock()
			delete(a.pendingAction, msg.From.ID)
			a.pendingMu.Unlock()
			switch action {
			case "sync_user":
				a.syncUserByID(ctx, b, msg, text)
			case "send_user":
				a.sendUserSubscriptionByID(ctx, b, msg, text)
			case "revoke_user":
				a.revokeUserByID(ctx, b, msg, text)
			}
			return
		}
	}
	if text == btnSubscribe {
		a.createSubscriptionRequest(ctx, b, msg)
		return
	}
	if text == btnGet {
		a.cmdGet(ctx, b, update)
		return
	}
	a.reply(ctx, b, msg, "Используй кнопки или команды /subscribe и /get.", nil)
}

func (a *App) handleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	q := update.CallbackQuery
	if q == nil {
		return
	}
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: q.ID})

	data := q.Data
	switch {
	case strings.HasPrefix(data, "approve:"), strings.HasPrefix(data, "reject:"):
		a.onDecision(ctx, b, update)
	case strings.HasPrefix(data, "cmd:"):
		a.onAction(ctx, b, update)
	}
}

func (a *App) onDecision(ctx context.Context, b *bot.Bot, update *models.Update) {
	q := update.CallbackQuery
	if q.From.ID == 0 || !a.IsApprover(q.From.ID) {
		if q.Message.Message != nil {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    q.Message.Message.Chat.ID,
				MessageID: q.Message.Message.ID,
				Text:      "Недостаточно прав.",
			})
		}
		return
	}
	parts := strings.SplitN(q.Data, ":", 2)
	if len(parts) != 2 {
		return
	}
	action, requestID := parts[0], parts[1]

	var (
		outcome string // notfound | processed | approved | rejected
		errMsg  string
		links   map[string]map[string]string
		entry   config.UserEntry
		req     config.Request
	)
	werr := a.svc.WithState(func(st *config.State) error {
		a.svc.SweepExpiredUsers(st, 0)
		r, ok := st.Requests[requestID]
		if !ok {
			outcome = "notfound"
			return nil
		}
		if r.Status != "pending" {
			outcome = "processed"
			return nil
		}
		r.RequestID = requestID
		req = r
		if action == "approve" {
			labelBase := req.Username
			if labelBase == "" {
				labelBase = fmt.Sprintf("user-%d", req.UserID)
			}
			userKey := strconv.FormatInt(req.UserID, 10)
			lm, err := a.svc.GetOrCreateUserLink(st, userKey, labelBase+"-happ")
			if err != nil {
				// Return the error so WithState rolls back: the request stays
				// pending and no partial state is written.
				errMsg = err.Error()
				return err
			}
			links = lm
			e := st.Users[userKey]
			if a.cfg.Bot.ApproverUserID != 0 && req.UserID == a.cfg.Bot.ApproverUserID {
				e.NeverExpires = true
				e.ExpiresAt = nil
			}
			a.svc.EnsureUserExpiry(&e)
			st.Users[userKey] = e
			entry = e
			r.Status = "approved"
			st.Requests[requestID] = r
			outcome = "approved"
			return nil
		}
		r.Status = "rejected"
		st.Requests[requestID] = r
		outcome = "rejected"
		return nil
	})

	switch outcome {
	case "notfound":
		a.editCallbackText(ctx, b, q, "Заявка не найдена.")
		return
	case "processed":
		a.editCallbackText(ctx, b, q, "Эта заявка уже обработана.")
		return
	}
	if errMsg != "" {
		a.editCallbackText(ctx, b, q, "Ошибка выдачи подписки: "+errMsg)
		return
	}
	if werr != nil {
		a.editCallbackText(ctx, b, q, "Ошибка сохранения состояния: "+werr.Error())
		return
	}

	if outcome == "approved" {
		text := "Заявка подтверждена.\n\n" + a.svc.BuildHappCodeMessage(links, a.svc.ExpiresAtInt(&entry))
		if panelID, ok := a.getPanel(req.UserID); ok {
			_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    req.UserID,
				MessageID: panelID,
				Text:      text,
				ParseMode: models.ParseModeMarkdownV1,
			})
			if err != nil {
				log.Printf("edit subscription panel failed user=%d: %v", req.UserID, err)
				_ = a.sendSubscription(ctx, b, req.UserID, text, nil)
			}
		} else {
			_ = a.sendSubscription(ctx, b, req.UserID, text, nil)
		}
		a.editCallbackText(ctx, b, q, "Заявка "+requestID+" подтверждена.")
		return
	}

	rejText := fmt.Sprintf("Заявка отклонена пользователем %s.", a.svc.ApproverLabel())
	if panelID, ok := a.getPanel(req.UserID); ok {
		_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: req.UserID, MessageID: panelID, Text: rejText,
		})
		if err != nil {
			a.send(ctx, b, req.UserID, rejText, nil)
		}
	} else {
		a.send(ctx, b, req.UserID, rejText, nil)
	}
	a.editCallbackText(ctx, b, q, "Заявка "+requestID+" отклонена.")
}

func (a *App) onAction(ctx context.Context, b *bot.Bot, update *models.Update) {
	q := update.CallbackQuery
	payload := strings.TrimPrefix(q.Data, "cmd:")
	parts := strings.Split(payload, ":")
	if len(parts) == 0 {
		return
	}
	action := parts[0]

	if action == "subscribe" {
		if q.Message.Message == nil {
			return
		}
		a.createSubscriptionRequestFromCallback(ctx, b, q)
		return
	}

	if q.From.ID == 0 || !a.IsApprover(q.From.ID) {
		if q.Message.Message != nil {
			a.reply(ctx, b, q.Message.Message, "Недостаточно прав.", nil)
		}
		return
	}
	msg := q.Message.Message

	switch action {
	case "get":
		label := q.From.Username
		if label == "" {
			label = "approver"
		}
		userKey := strconv.FormatInt(q.From.ID, 10)
		var (
			links map[string]map[string]string
			entry config.UserEntry
		)
		if err := a.svc.WithState(func(st *config.State) error {
			a.svc.SweepExpiredUsers(st, 0)
			lm, err := a.svc.GetOrCreateUserLink(st, userKey, label+"-happ")
			if err != nil {
				return err
			}
			links = lm
			e := st.Users[userKey]
			if a.IsApprover(q.From.ID) {
				e.NeverExpires = true
				e.ExpiresAt = nil
			}
			a.svc.EnsureUserExpiry(&e)
			st.Users[userKey] = e
			entry = e
			return nil
		}); err != nil {
			a.reply(ctx, b, msg, "Ошибка: "+err.Error(), nil)
			return
		}
		a.replyMD(ctx, b, msg, a.svc.BuildHappCodeMessage(links, a.svc.ExpiresAtInt(&entry)), AdminMenuKeyboard())
	case "create":
		a.pendingMu.Lock()
		a.pendingAction[q.From.ID] = "create_user"
		a.pendingMu.Unlock()
		a.reply(ctx, b, msg, "Отправь данные нового пользователя одной строкой:\n<user_id> [days|never] [label]\n\nПримеры:\n123456789\n123456789 30\n123456789 never my-label", nil)
	case "admin":
		a.reply(ctx, b, msg, "Панель управления:", AdminMenuKeyboard())
	case "pending":
		a.sendPendingRequests(ctx, b, msg.Chat.ID)
	case "users":
		text, err := a.FormatUsersMessage()
		if err == nil {
			a.reply(ctx, b, msg, text, AdminMenuKeyboard())
		}
	case "servers":
		a.reply(ctx, b, msg, a.FormatServersMessage(), AdminMenuKeyboard())
	case "health":
		text, err := a.FormatHealthMessage()
		if err == nil {
			a.reply(ctx, b, msg, text, AdminMenuKeyboard())
		}
	case "traffic":
		a.reply(ctx, b, msg, a.FormatTrafficMessage(), AdminMenuKeyboard())
	case "userpick":
		if len(parts) >= 3 {
			a.showUserPicker(ctx, b, msg, parts[1], atoi(parts[2]))
		}
	case "userdo":
		if len(parts) >= 3 {
			a.handleUserDo(ctx, b, msg, parts[1], parts[2])
		}
	case "renew":
		if len(parts) >= 3 {
			a.renewUserByID(ctx, b, msg, parts[1], atoi(parts[2]))
		}
	case "never":
		if len(parts) >= 3 {
			a.neverExpiresByID(ctx, b, msg, parts[1], parts[2] == "1")
		}
	default:
		a.reply(ctx, b, msg, "Неизвестная команда.", AdminMenuKeyboard())
	}
}

func (a *App) createSubscriptionRequestFromCallback(ctx context.Context, b *bot.Bot, q *models.CallbackQuery) {
	msg := q.Message.Message
	if msg == nil || q.From.ID == 0 {
		return
	}
	now := time.Now().Unix()
	requestID := fmt.Sprintf("%d-%d", q.From.ID, now)
	req := config.Request{
		RequestID: requestID,
		UserID:    q.From.ID,
		Username:  q.From.Username,
		FirstName: q.From.FirstName,
		Status:    "pending",
		CreatedAt: now,
	}
	var approverChatID *int64
	if err := a.svc.WithState(func(st *config.State) error {
		st.Requests[requestID] = req
		approverChatID = st.ApproverChatID
		return nil
	}); err != nil {
		a.reply(ctx, b, msg, "Не удалось сохранить заявку: "+err.Error(), nil)
		return
	}
	var text string
	if approverChatID == nil {
		text = fmt.Sprintf(
			"Заявка создана, но %s еще не открыл чат с ботом.\nПопроси %s написать боту /start.",
			a.svc.ApproverLabel(), a.svc.ApproverLabel(),
		)
	} else {
		username := "(без username)"
		if req.Username != "" {
			username = "@" + req.Username
		}
		textToApprover := fmt.Sprintf(
			"Новая заявка на подписку\nID: %s\nUser: %s\nUsername: %s\nUser ID: %d",
			req.RequestID, req.FirstName, username, req.UserID,
		)
		a.send(ctx, b, *approverChatID, textToApprover, ApproveRejectKeyboard(requestID))
		text = "Заявка отправлена на подтверждение."
	}
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, Text: text, ReplyMarkup: UserKeyboard(),
	})
}

func (a *App) sendPendingRequests(ctx context.Context, b *bot.Bot, chatID int64) {
	st, err := a.svc.LoadState()
	if err != nil {
		return
	}
	pending := pendingRequests(st)
	if len(pending) == 0 {
		a.send(ctx, b, chatID, "Нет заявок в ожидании.", AdminMenuKeyboard())
		return
	}
	a.send(ctx, b, chatID, fmt.Sprintf("Заявок в ожидании: %d", len(pending)), nil)
	for _, req := range pending {
		username := "(без username)"
		if req.Username != "" {
			username = "@" + req.Username
		}
		text := fmt.Sprintf(
			"Заявка %s\nUser: %s\nUsername: %s\nUser ID: %d",
			req.RequestID, req.FirstName, username, req.UserID,
		)
		a.send(ctx, b, chatID, text, ApproveRejectKeyboard(req.RequestID))
	}
	a.send(ctx, b, chatID, btnMenu, AdminMenuKeyboard())
}

func (a *App) showUserPicker(ctx context.Context, b *bot.Bot, msg *models.Message, action string, page int) {
	st, err := a.svc.LoadState()
	if err != nil {
		return
	}
	if len(st.Users) == 0 {
		a.reply(ctx, b, msg, "Выданных пользователей пока нет.", nil)
		return
	}
	a.reply(ctx, b, msg, fmt.Sprintf("Выбери пользователя для %s:", userActionTitle(action)),
		BuildUserPickerKeyboard(st, action, page))
}

func (a *App) handleUserDo(ctx context.Context, b *bot.Bot, msg *models.Message, action, targetUserID string) {
	switch action {
	case "sync":
		a.syncUserByID(ctx, b, msg, targetUserID)
	case "send":
		a.sendUserSubscriptionByID(ctx, b, msg, targetUserID)
	case "revoke":
		a.revokeUserByID(ctx, b, msg, targetUserID)
	case "renew":
		st, err := a.svc.LoadState()
		if err != nil {
			return
		}
		if _, ok := st.Users[targetUserID]; !ok {
			a.reply(ctx, b, msg, "Пользователь не найден.", nil)
			return
		}
		a.reply(ctx, b, msg, fmt.Sprintf("Продлить подписку пользователя %s:", targetUserID),
			BuildRenewDaysKeyboard(targetUserID))
	case "never":
		st, err := a.svc.LoadState()
		if err != nil {
			return
		}
		userData, ok := st.Users[targetUserID]
		if !ok {
			a.reply(ctx, b, msg, "Пользователь не найден.", nil)
			return
		}
		a.reply(ctx, b, msg, fmt.Sprintf("Бессрочная подписка для %s:", targetUserID),
			BuildNeverToggleKeyboard(targetUserID, userData.NeverExpires))
	}
}

func (a *App) syncUserByID(ctx context.Context, b *bot.Bot, msg *models.Message, targetUserID string) {
	added, err := a.svc.SyncUser(targetUserID)
	if err != nil {
		a.reply(ctx, b, msg, "Ошибка синхронизации: "+err.Error(), nil)
		return
	}
	if len(added) > 0 {
		a.reply(ctx, b, msg, fmt.Sprintf("Пользователь %s синхронизирован. Добавлены: %s", targetUserID, strings.Join(added, ", ")), AdminMenuKeyboard())
		return
	}
	a.reply(ctx, b, msg, fmt.Sprintf("Пользователь %s уже есть на всех серверах.", targetUserID), AdminMenuKeyboard())
}

func (a *App) revokeUserByID(ctx context.Context, b *bot.Bot, msg *models.Message, targetUserID string) {
	removed, err := a.svc.RevokeUser(targetUserID)
	if err != nil {
		a.reply(ctx, b, msg, "Ошибка удаления: "+err.Error(), nil)
		return
	}
	if len(removed) > 0 {
		a.reply(ctx, b, msg, fmt.Sprintf("Пользователь %s удален из state и Xray на: %s", targetUserID, strings.Join(removed, ", ")), AdminMenuKeyboard())
		return
	}
	a.reply(ctx, b, msg, fmt.Sprintf("Пользователь %s удален из state.", targetUserID), AdminMenuKeyboard())
}

func (a *App) sendUserSubscriptionByID(ctx context.Context, b *bot.Bot, msg *models.Message, targetUserID string) {
	var (
		servers map[string]map[string]string
		expiry  *int64
	)
	err := a.svc.WithState(func(st *config.State) error {
		userData, ok := st.Users[targetUserID]
		if !ok || len(userData.Servers) == 0 {
			return fmt.Errorf("пользователь не найден или ссылки отсутствуют")
		}
		if a.svc.IsUserExpired(&userData, 0) {
			return fmt.Errorf("подписка пользователя истекла")
		}
		a.svc.EnsureUserExpiry(&userData)
		st.Users[targetUserID] = userData
		servers = userData.Servers
		expiry = a.svc.ExpiresAtInt(&userData)
		return nil
	})
	if err != nil {
		a.reply(ctx, b, msg, err.Error(), nil)
		return
	}
	targetID, _ := strconv.ParseInt(targetUserID, 10, 64)
	text := "Твоя подписка (повторная отправка):\n\n" + a.svc.BuildHappCodeMessage(servers, expiry)
	if err := a.sendSubscription(ctx, b, targetID, text, nil); err != nil {
		a.reply(ctx, b, msg, "Не удалось отправить сообщение: "+err.Error(), nil)
		return
	}
	a.reply(ctx, b, msg, fmt.Sprintf("Подписка отправлена пользователю %s.", targetUserID), AdminMenuKeyboard())
}

func (a *App) renewUserByID(ctx context.Context, b *bot.Bot, msg *models.Message, targetUserID string, days int) {
	exp, err := a.svc.RenewUser(targetUserID, days)
	if err != nil {
		a.reply(ctx, b, msg, "Ошибка продления: "+err.Error(), nil)
		return
	}
	a.reply(ctx, b, msg, fmt.Sprintf(
		"Подписка пользователя %s продлена на %d дн.\nИстекает: %s",
		targetUserID, days, a.svc.FormatTS(exp),
	), AdminMenuKeyboard())
}

func (a *App) neverExpiresByID(ctx context.Context, b *bot.Bot, msg *models.Message, targetUserID string, value bool) {
	if err := a.svc.SetNeverExpires(targetUserID, value); err != nil {
		a.reply(ctx, b, msg, "Ошибка: "+err.Error(), nil)
		return
	}
	status := "отключена"
	if value {
		status = "включена"
	}
	a.reply(ctx, b, msg, fmt.Sprintf("Бессрочная подписка для %s %s.", targetUserID, status), AdminMenuKeyboard())
}

func (a *App) createUserByInput(ctx context.Context, b *bot.Bot, msg *models.Message, input string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		a.reply(ctx, b, msg, "Использование: <user_id> [days|never] [label]", nil)
		return
	}
	userID := fields[0]
	if !isDigits(userID) {
		a.reply(ctx, b, msg, "user_id должен быть числом.", nil)
		return
	}
	var (
		never bool
		days  int
	)
	rest := fields[1:]
	if len(rest) > 0 {
		switch strings.ToLower(rest[0]) {
		case "never", "∞", "♾":
			never = true
			rest = rest[1:]
		default:
			if n, err := strconv.Atoi(rest[0]); err == nil && n > 0 {
				days = n
				rest = rest[1:]
			}
		}
	}
	label := strings.Join(rest, " ")
	a.createUser(ctx, b, msg, userID, label, never, days)
}

func (a *App) createUser(ctx context.Context, b *bot.Bot, msg *models.Message, userID, label string, never bool, days int) {
	links, err := a.svc.AddUser(userID, label, never, days)
	if err != nil {
		a.reply(ctx, b, msg, "Ошибка создания пользователя: "+err.Error(), nil)
		return
	}
	st, err := a.svc.LoadState()
	if err != nil {
		a.reply(ctx, b, msg, "Пользователь создан, но не удалось прочитать state: "+err.Error(), nil)
		return
	}
	entry := st.Users[userID]
	expiry := a.svc.ExpiresAtInt(&entry)
	var expiresView string
	if entry.NeverExpires {
		expiresView = "бессрочно"
	} else if expiry != nil && *expiry > 0 {
		expiresView = a.svc.FormatTS(*expiry)
	} else {
		expiresView = "n/a"
	}
	head := fmt.Sprintf("Пользователь %s создан. Истекает: %s\n\n", userID, expiresView)
	a.sendMD(ctx, b, msg.Chat.ID, head+a.svc.BuildHappCodeMessage(links, expiry), AdminMenuKeyboard())
}

func (a *App) editCallbackText(ctx context.Context, b *bot.Bot, q *models.CallbackQuery, text string) {
	if q.Message.Message == nil {
		return
	}
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: q.Message.Message.Chat.ID, MessageID: q.Message.Message.ID, Text: text,
	})
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
