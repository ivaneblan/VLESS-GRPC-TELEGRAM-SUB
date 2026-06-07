package botapp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

const (
	btnSubscribe = "📨 Запросить подписку"
	btnAdmin     = "🛠 Админ-панель"
	btnGet       = "🔑 Подписка (/get)"
	btnCreate    = "➕ Создать пользователя"
	btnPending   = "📥 Заявки"
	btnUsers     = "👥 Пользователи"
	btnServers   = "🖥 Серверы"
	btnSync      = "🔄 Синхрон user_id"
	btnRevoke    = "🗑 Удалить user_id"
	btnSend      = "✉️ Отправить user_id"
	btnHealth    = "📊 Сводка"
	btnTraffic   = "📈 Трафик серверов"
	btnRenew     = "⏳ Продлить"
	btnNever     = "♾ Бессрочно"
	btnMenu      = "🏠 Меню"
	userPickPage = 8
)

func AdminMenuKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: btnGet, CallbackData: "cmd:get"}},
		{{Text: btnCreate, CallbackData: "cmd:create"}},
		{
			{Text: btnPending, CallbackData: "cmd:pending"},
			{Text: btnHealth, CallbackData: "cmd:health"},
		},
		{
			{Text: btnUsers, CallbackData: "cmd:users"},
			{Text: btnTraffic, CallbackData: "cmd:traffic"},
		},
		{{Text: btnServers, CallbackData: "cmd:servers"}},
		{
			{Text: btnSync, CallbackData: "cmd:userpick:sync:0"},
			{Text: btnSend, CallbackData: "cmd:userpick:send:0"},
		},
		{
			{Text: btnRevoke, CallbackData: "cmd:userpick:revoke:0"},
			{Text: btnRenew, CallbackData: "cmd:userpick:renew:0"},
		},
		{{Text: btnNever, CallbackData: "cmd:userpick:never:0"}},
	}}
}

func UserKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: btnSubscribe, CallbackData: "cmd:subscribe"}},
	}}
}

func sortedUserIDs(st *config.State) []string {
	type pair struct {
		id  string
		ats int64
	}
	var pairs []pair
	for id, info := range st.Users {
		pairs = append(pairs, pair{id: id, ats: info.CreatedAt})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ats > pairs[j].ats })
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.id
	}
	return out
}

func BuildUserPickerKeyboard(st *config.State, action string, page int) *models.InlineKeyboardMarkup {
	userIDs := sortedUserIDs(st)
	start := page * userPickPage
	end := start + userPickPage
	if start > len(userIDs) {
		start = len(userIDs)
	}
	if end > len(userIDs) {
		end = len(userIDs)
	}
	pageIDs := userIDs[start:end]

	var rows [][]models.InlineKeyboardButton
	for _, userID := range pageIDs {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "user_id " + userID, CallbackData: fmt.Sprintf("cmd:userdo:%s:%s", action, userID)},
		})
	}
	var nav []models.InlineKeyboardButton
	if page > 0 {
		nav = append(nav, models.InlineKeyboardButton{
			Text: "⬅️", CallbackData: fmt.Sprintf("cmd:userpick:%s:%d", action, page-1),
		})
	}
	if end < len(userIDs) {
		nav = append(nav, models.InlineKeyboardButton{
			Text: "➡️", CallbackData: fmt.Sprintf("cmd:userpick:%s:%d", action, page+1),
		})
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, []models.InlineKeyboardButton{{Text: btnMenu, CallbackData: "cmd:admin"}})
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func BuildRenewDaysKeyboard(userID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: "+7 дн", CallbackData: fmt.Sprintf("cmd:renew:%s:7", userID)},
			{Text: "+30 дн", CallbackData: fmt.Sprintf("cmd:renew:%s:30", userID)},
			{Text: "+90 дн", CallbackData: fmt.Sprintf("cmd:renew:%s:90", userID)},
		},
		{{Text: btnMenu, CallbackData: "cmd:admin"}},
	}}
}

func BuildNeverToggleKeyboard(userID string, enabled bool) *models.InlineKeyboardMarkup {
	var btn models.InlineKeyboardButton
	if enabled {
		btn = models.InlineKeyboardButton{Text: "❌ Отключить бессрочно", CallbackData: fmt.Sprintf("cmd:never:%s:0", userID)}
	} else {
		btn = models.InlineKeyboardButton{Text: "✅ Включить бессрочно", CallbackData: fmt.Sprintf("cmd:never:%s:1", userID)}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{btn},
		{{Text: btnMenu, CallbackData: "cmd:admin"}},
	}}
}

func ApproveRejectKeyboard(requestID string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: "✅ Approve", CallbackData: "approve:" + requestID},
			{Text: "❌ Reject", CallbackData: "reject:" + requestID},
		},
	}}
}

func (a *App) FormatHealthMessage() (string, error) {
	st, err := a.svc.LoadState()
	if err != nil {
		return "", err
	}
	pending := 0
	for _, req := range st.Requests {
		if req.Status == "pending" {
			pending++
		}
	}
	nowTS := time.Now().Unix()
	expired := 0
	for _, user := range st.Users {
		u := user
		if a.svc.IsUserExpired(&u, nowTS) {
			expired++
		}
	}
	return fmt.Sprintf(
		"Сводка:\n- серверов: %d\n- пользователей: %d\n- заявок pending: %d\n- просроченных подписок: %d",
		len(a.cfg.Servers), len(st.Users), pending, expired,
	), nil
}

func (a *App) FormatUsersMessage() (string, error) {
	st, err := a.svc.LoadState()
	if err != nil {
		return "", err
	}
	if len(st.Users) == 0 {
		return "Выданных пользователей пока нет.", nil
	}
	type row struct {
		id  string
		ats int64
	}
	var rows []row
	for id, info := range st.Users {
		rows = append(rows, row{id: id, ats: info.CreatedAt})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ats > rows[j].ats })

	lines := []string{"Выданные пользователи:"}
	for _, r := range rows {
		info := st.Users[r.id]
		a.svc.EnsureUserExpiry(&info)
		label := info.Label
		if label == "" {
			label = "no-label"
		}
		clientUUID := info.UUID
		if clientUUID == "" {
			clientUUID = "no-uuid"
		}
		hosts := make([]string, 0, len(info.Servers))
		for host := range info.Servers {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		serversView := strings.Join(hosts, ",")
		if serversView == "" {
			serversView = "-"
		}
		var expiresView string
		if info.NeverExpires {
			expiresView = "never"
		} else if info.ExpiresAt != nil && *info.ExpiresAt > 0 {
			expiresView = a.svc.FormatTS(*info.ExpiresAt)
		} else {
			expiresView = "n/a"
		}
		lines = append(lines, fmt.Sprintf(
			"- user_id: %s | label: %s | uuid: %s | expires: %s | servers: %s",
			r.id, label, clientUUID, expiresView, serversView,
		))
	}
	return strings.Join(lines, "\n"), nil
}

func (a *App) FormatServersMessage() string {
	x := a.cfg.Xray.WithDefaults()
	lines := []string{"Список серверов:"}
	for i, server := range a.cfg.Servers {
		name := server.Name
		if name == "" {
			name = server.Host
		}
		lines = append(lines, fmt.Sprintf("%d. %s (%s) | sni=%s | port=%d", i+1, name, server.Host, x.SNI, x.Port))
	}
	return strings.Join(lines, "\n")
}

func (a *App) FormatTrafficMessage() string {
	lines := []string{"Трафик по серверам (с момента старта сервера):"}
	for _, server := range a.cfg.Servers {
		name := server.Name
		if name == "" {
			name = server.Host
		}
		stats, err := a.svc.GetServerTrafficStats(&server)
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: ошибка чтения статистики (%v)", name, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: iface=%s | RX=%s GB | TX=%s GB", name, stats.Iface, stats.RXGB, stats.TXGB))
	}
	return strings.Join(lines, "\n")
}

func pendingRequests(st *config.State) []config.Request {
	var out []config.Request
	for id, req := range st.Requests {
		if req.Status != "pending" {
			continue
		}
		if req.RequestID == "" {
			req.RequestID = id
		}
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func userActionTitle(action string) string {
	switch action {
	case "sync":
		return "синхронизации"
	case "send":
		return "повторной отправки"
	case "revoke":
		return "удаления"
	case "renew":
		return "продления"
	case "never":
		return "бессрочной подписки"
	default:
		return action
	}
}

func isDigits(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}
