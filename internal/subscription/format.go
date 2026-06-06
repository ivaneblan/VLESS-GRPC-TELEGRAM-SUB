package subscription

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

func (m *Manager) BuildSubscriptionMessage(serverLinks map[string]map[string]string, expiresAt *int64) string {
	lines := []string{"smknVpn server list:\n"}
	if expiresAt != nil && *expiresAt > 0 {
		lines = append(lines, fmt.Sprintf("Subscription valid until: %s", m.FormatTS(*expiresAt)))
		lines = append(lines, "")
	}
	links := m.DefaultLinks(serverLinks)
	if len(links) > 0 {
		lines = append(lines, "VLESS Reality (gRPC):")
		lines = append(lines, "```\n"+strings.Join(links, "\n")+"\n```")
		lines = append(lines, "")
	}
	lines = append(lines, "В Happ: Add profile -> Import from clipboard/URL.")
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
