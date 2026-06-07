package subscription

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

func (m *Manager) BuildSubscriptionMessage(serverLinks map[string]map[string]string, expiresAt *int64) string {
	title := strings.TrimSpace(m.Cfg.SubscriptionTitle)
	if title == "" {
		title = "smknVPN"
	}
	lines := []string{"smknVpn — ваша подписка:\n"}
	if expiresAt != nil && *expiresAt > 0 {
		lines = append(lines, fmt.Sprintf("Подписка действует до: %s", m.FormatTS(*expiresAt)))
		lines = append(lines, "")
	}

	// One combined subscription body: bridges first (обход ТСПУ через РФ —
	// рекомендуется для домашнего интернета), затем прямые exit-серверы.
	var bridges, exits []config.ServerDef
	for _, s := range m.Cfg.Servers {
		if s.IsBridge() {
			bridges = append(bridges, s)
		} else {
			exits = append(exits, s)
		}
	}
	ordered := append(append([]config.ServerDef{}, bridges...), exits...)

	var body []string
	for _, s := range ordered {
		hostLinks := serverLinks[s.Host]
		if hostLinks == nil {
			continue
		}
		if link := hostLinks["default"]; link != "" {
			body = append(body, link)
		}
	}
	if len(body) == 0 {
		// Fallback: state built before servers were known.
		body = m.DefaultLinks(serverLinks)
	}

	if len(body) > 0 {
		// Happ subscription body: a `#profile-title` header groups the links
		// under one named subscription on import from clipboard.
		block := "#profile-title: " + title + "\n" + strings.Join(body, "\n")
		lines = append(lines, "Все серверы одной подпиской (Happ):")
		lines = append(lines, "```\n"+block+"\n```")
		lines = append(lines, "")
	}

	lines = append(lines, fmt.Sprintf("В Happ: Add profile -> Import from clipboard/URL. Серверы появятся группой «%s».", title))
	return strings.Join(lines, "\n")
}

func (m *Manager) FormatUsersList(st *config.State) string {
	if len(st.Users) == 0 {
		return "No users."
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

	lines := []string{"Users:"}
	for _, r := range rows {
		info := st.Users[r.id]
		m.EnsureUserExpiry(&info)
		label := info.Label
		if label == "" {
			label = "-"
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
			expiresView = m.FormatTS(*info.ExpiresAt)
		} else {
			expiresView = "n/a"
		}
		lines = append(lines, fmt.Sprintf(
			"- %s | label=%s | uuid=%s | expires=%s | servers=%s",
			r.id, label, info.UUID, expiresView, serversView,
		))
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) FormatServersList() string {
	x := m.Cfg.Xray.WithDefaults()
	lines := []string{"Servers:"}
	for i, server := range m.Cfg.Servers {
		name := server.Name
		if name == "" {
			name = server.ID
		}
		bot := ""
		if m.Cfg.Bot.ServerID == server.ID {
			bot = " [bot]"
		}
		lines = append(lines, fmt.Sprintf(
			"%d. %s (%s) host=%s port=%d sni=%s%s",
			i+1, name, server.ID, server.Host, x.Port, x.SNI, bot,
		))
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) FormatTraffic() string {
	lines := []string{"Server traffic (since boot):"}
	for _, server := range m.Cfg.Servers {
		name := server.Name
		if name == "" {
			name = server.ID
		}
		stats, err := m.GetServerTrafficStats(&server)
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: error (%v)", name, err))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"- %s: iface=%s RX=%s GB TX=%s GB",
			name, stats.Iface, stats.RXGB, stats.TXGB,
		))
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) FormatSummary(st *config.State) string {
	pending := 0
	for _, req := range st.Requests {
		if m, ok := req.(map[string]interface{}); ok {
			if s, _ := m["status"].(string); s == "pending" {
				pending++
			}
		}
	}
	nowTS := int64(0)
	expired := 0
	for _, user := range st.Users {
		u := user
		if m.IsUserExpired(&u, nowTS) {
			expired++
		}
	}
	return fmt.Sprintf(
		"Summary:\n- servers: %d\n- users: %d\n- pending requests: %d\n- expired subscriptions: %d",
		len(m.Cfg.Servers), len(st.Users), pending, expired,
	)
}

func FormatLinksPlain(serverLinks map[string]map[string]string, defaultOnly bool) string {
	hosts := make([]string, 0, len(serverLinks))
	for host := range serverLinks {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	var lines []string
	for _, host := range hosts {
		links := serverLinks[host]
		if defaultOnly {
			if link := links["default"]; link != "" {
				lines = append(lines, link)
			}
			continue
		}
		keys := make([]string, 0, len(links))
		for k := range links {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("[%s/%s] %s", host, k, links[k]))
		}
	}
	return strings.Join(lines, "\n")
}

func FormatLinksJSON(serverLinks map[string]map[string]string) (string, error) {
	data, err := json.MarshalIndent(serverLinks, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
