package subscription

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/links"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/xray"
)

const expirySweepIntervalSec = 90

type Manager struct {
	Cfg   *config.Config
	Sec   *config.Secrets
	Paths config.Paths
	mu    sync.Mutex
}

func NewManager(cfg *config.Config, sec *config.Secrets, paths config.Paths) *Manager {
	return &Manager{Cfg: cfg, Sec: sec, Paths: paths}
}

func (m *Manager) LoadState() (*config.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return config.LoadState(m.Paths.StatePath)
}

func (m *Manager) SaveState(st *config.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return config.SaveState(m.Paths.StatePath, st)
}

// WithState is the single atomic mutation primitive: it serialises in-process
// callers via m.mu and guards cross-process access via an OS file lock, then
// performs a read-modify-write of state.yaml. fn must not call LoadState,
// SaveState or WithState again (the mutex is not reentrant).
func (m *Manager) WithState(fn func(*config.State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return config.MutateState(m.Paths.StatePath, fn)
}

func (m *Manager) FormatTS(ts int64) string {
	return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04 UTC")
}

func (m *Manager) EnsureUserExpiry(entry *config.UserEntry) {
	if entry.NeverExpires {
		entry.ExpiresAt = nil
		return
	}
	if entry.ExpiresAt != nil && *entry.ExpiresAt > 0 {
		return
	}
	created := entry.CreatedAt
	if created == 0 {
		created = time.Now().Unix()
	}
	exp := created + int64(m.Cfg.Bot.DefaultSubscriptionDays)*24*3600
	entry.ExpiresAt = &exp
}

func (m *Manager) IsUserExpired(entry *config.UserEntry, nowTS int64) bool {
	if entry.NeverExpires {
		return false
	}
	if nowTS == 0 {
		nowTS = time.Now().Unix()
	}
	m.EnsureUserExpiry(entry)
	if entry.ExpiresAt == nil {
		return false
	}
	return *entry.ExpiresAt > 0 && *entry.ExpiresAt <= nowTS
}

func (m *Manager) ExpiresAtInt(entry *config.UserEntry) *int64 {
	if entry.NeverExpires {
		return nil
	}
	m.EnsureUserExpiry(entry)
	return entry.ExpiresAt
}

type SweepResult struct {
	OK           bool
	Skipped      bool
	RemovedUsers []string
}

func (m *Manager) SweepExpiredUsers(st *config.State, nowTS int64) SweepResult {
	if nowTS == 0 {
		nowTS = time.Now().Unix()
	}
	last := st.LastExpirySweepAt
	if expirySweepIntervalSec > 0 && (nowTS-last) < expirySweepIntervalSec {
		return SweepResult{OK: true, Skipped: true}
	}

	var removed []string
	for userID, info := range st.Users {
		if m.Cfg.Bot.ApproverUserID != 0 && strconv.FormatInt(m.Cfg.Bot.ApproverUserID, 10) == userID {
			info.NeverExpires = true
			info.ExpiresAt = nil
			st.Users[userID] = info
			continue
		}
		if !m.IsUserExpired(&info, nowTS) {
			continue
		}
		clientUUID := strings.TrimSpace(info.UUID)
		if clientUUID != "" {
			for i := range m.Cfg.Servers {
				_, _ = m.RemoveXrayClient(&m.Cfg.Servers[i], clientUUID)
			}
		}
		delete(st.Users, userID)
		removed = append(removed, userID)
	}
	st.LastExpirySweepAt = nowTS
	return SweepResult{OK: true, RemovedUsers: removed}
}

func (m *Manager) connect(server *config.ServerDef) (*sshclient.Client, error) {
	return sshclient.Connect(server.Host, m.Sec, server.ID)
}

func (m *Manager) UpsertXrayClient(server *config.ServerDef, clientUUID string) error {
	client, err := m.connect(server)
	if err != nil {
		return err
	}
	defer client.Close()
	return xray.UpsertClient(client, clientUUID, m.Cfg.Xray.Flow)
}

func (m *Manager) RemoveXrayClient(server *config.ServerDef, clientUUID string) (bool, error) {
	client, err := m.connect(server)
	if err != nil {
		return false, err
	}
	defer client.Close()
	return xray.RemoveClient(client, clientUUID)
}

func (m *Manager) BuildServerLinks(server *config.ServerDef, clientUUID string) (map[string]string, error) {
	params, err := links.ParamsFromConfig(m.Cfg, m.Sec, server)
	if err != nil {
		return nil, err
	}
	return links.BuildServerLinks(params, clientUUID), nil
}

// GetOrCreateUserLink ensures the user exists with an xray client on every
// server and returns the per-server links. It mutates st in place and performs
// remote xray writes, but does NOT persist state: callers must run it inside
// WithState (or save st themselves) so the whole operation stays atomic.
func (m *Manager) GetOrCreateUserLink(st *config.State, userID, label string) (map[string]map[string]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("empty user id")
	}
	info, exists := st.Users[userID]

	if exists {
		existingUUID := strings.TrimSpace(info.UUID)
		if existingUUID == "" && len(info.Servers) > 0 {
			return nil, fmt.Errorf("user %s has servers but missing uuid in state", userID)
		}
		if existingUUID != "" {
			if info.Servers == nil {
				info.Servers = map[string]map[string]string{}
			}
			for _, server := range m.Cfg.Servers {
				if err := m.UpsertXrayClient(&server, existingUUID); err != nil {
					return nil, err
				}
				linksMap, err := m.BuildServerLinks(&server, existingUUID)
				if err != nil {
					return nil, err
				}
				info.Servers[server.Host] = linksMap
			}
			st.Users[userID] = info
			return info.Servers, nil
		}
	}

	clientUUID := uuid.New().String()
	serverLinks := map[string]map[string]string{}
	for _, server := range m.Cfg.Servers {
		if err := m.UpsertXrayClient(&server, clientUUID); err != nil {
			return nil, err
		}
		linksMap, err := m.BuildServerLinks(&server, clientUUID)
		if err != nil {
			return nil, err
		}
		serverLinks[server.Host] = linksMap
	}
	now := time.Now().Unix()
	st.Users[userID] = config.UserEntry{
		UUID:      clientUUID,
		Label:     label,
		CreatedAt: now,
		Servers:   serverLinks,
	}
	return serverLinks, nil
}

func (m *Manager) AddUser(userID, label string, never bool, days int) (map[string]map[string]string, error) {
	var linksMap map[string]map[string]string
	err := m.WithState(func(st *config.State) error {
		m.SweepExpiredUsers(st, 0)
		lbl := label
		if lbl == "" {
			lbl = fmt.Sprintf("user-%s-happ", userID)
		}
		lm, err := m.GetOrCreateUserLink(st, userID, lbl)
		if err != nil {
			return err
		}
		linksMap = lm
		entry := st.Users[userID]
		if never {
			entry.NeverExpires = true
			entry.ExpiresAt = nil
		} else if days > 0 {
			exp := time.Now().Unix() + int64(days)*24*3600
			entry.ExpiresAt = &exp
			entry.NeverExpires = false
		} else {
			m.EnsureUserExpiry(&entry)
		}
		st.Users[userID] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return linksMap, nil
}

func (m *Manager) RevokeUser(userID string) ([]string, error) {
	var removed []string
	err := m.WithState(func(st *config.State) error {
		userData, ok := st.Users[userID]
		if !ok {
			return fmt.Errorf("user %s not found", userID)
		}
		clientUUID := strings.TrimSpace(userData.UUID)
		if clientUUID != "" {
			for _, server := range m.Cfg.Servers {
				ok, err := m.RemoveXrayClient(&server, clientUUID)
				if err != nil {
					return fmt.Errorf("%s: %w", server.Host, err)
				}
				if ok {
					removed = append(removed, server.Host)
				}
			}
		}
		delete(st.Users, userID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

func (m *Manager) SyncUser(userID string) ([]string, error) {
	var added []string
	err := m.WithState(func(st *config.State) error {
		userData, ok := st.Users[userID]
		if !ok {
			return fmt.Errorf("user %s not found", userID)
		}
		clientUUID := strings.TrimSpace(userData.UUID)
		if clientUUID == "" {
			return fmt.Errorf("user %s has no uuid", userID)
		}
		if userData.Servers == nil {
			userData.Servers = map[string]map[string]string{}
		}
		for _, server := range m.Cfg.Servers {
			if _, exists := userData.Servers[server.Host]; exists {
				continue
			}
			if err := m.UpsertXrayClient(&server, clientUUID); err != nil {
				return err
			}
			linksMap, err := m.BuildServerLinks(&server, clientUUID)
			if err != nil {
				return err
			}
			userData.Servers[server.Host] = linksMap
			added = append(added, server.Host)
		}
		st.Users[userID] = userData
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

func (m *Manager) RenewUser(userID string, days int) (int64, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be positive")
	}
	var exp int64
	err := m.WithState(func(st *config.State) error {
		m.SweepExpiredUsers(st, 0)
		userData, ok := st.Users[userID]
		if !ok {
			return fmt.Errorf("user %s not found", userID)
		}
		if userData.NeverExpires {
			return fmt.Errorf("user %s has never-expires enabled", userID)
		}
		nowTS := time.Now().Unix()
		var current int64
		if userData.ExpiresAt != nil {
			current = *userData.ExpiresAt
		}
		base := current
		if base <= nowTS {
			base = nowTS
		}
		exp = base + int64(days)*24*3600
		userData.ExpiresAt = &exp
		st.Users[userID] = userData
		return nil
	})
	if err != nil {
		return 0, err
	}
	return exp, nil
}

func (m *Manager) SetNeverExpires(userID string, value bool) error {
	return m.WithState(func(st *config.State) error {
		m.SweepExpiredUsers(st, 0)
		userData, ok := st.Users[userID]
		if !ok {
			return fmt.Errorf("user %s not found", userID)
		}
		userData.NeverExpires = value
		if value {
			userData.ExpiresAt = nil
		} else {
			m.EnsureUserExpiry(&userData)
		}
		st.Users[userID] = userData
		return nil
	})
}

type TrafficStats struct {
	Iface string
	RXGB  string
	TXGB  string
}

func (m *Manager) GetServerTrafficStats(server *config.ServerDef) (TrafficStats, error) {
	client, err := m.connect(server)
	if err != nil {
		return TrafficStats{}, err
	}
	defer client.Close()

	preferredIface := ""
	rc, defaultOut, _ := client.Run("ip route show default", 30*time.Second)
	if rc == 0 && strings.TrimSpace(defaultOut) != "" {
		parts := strings.Fields(strings.TrimSpace(defaultOut))
		for i, p := range parts {
			if p == "dev" && i+1 < len(parts) {
				preferredIface = parts[i+1]
				break
			}
		}
	}

	rc, devOut, devErr := client.Run("cat /proc/net/dev", 30*time.Second)
	if rc != 0 {
		return TrafficStats{}, fmt.Errorf("%s", strings.TrimSpace(devErr+devOut))
	}

	type ifaceStat struct {
		rx, tx int64
	}
	stats := map[string]ifaceStat{}
	for _, rawLine := range strings.Split(devOut, "\n") {
		if !strings.Contains(rawLine, ":") {
			continue
		}
		parts := strings.SplitN(rawLine, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[8], 10, 64)
		stats[iface] = ifaceStat{rx: rx, tx: tx}
	}
	if len(stats) == 0 {
		return TrafficStats{}, fmt.Errorf("no interfaces parsed from /proc/net/dev")
	}

	iface := preferredIface
	if _, ok := stats[iface]; !ok {
		iface = ""
	}
	if iface == "" {
		var best string
		var bestSum int64
		for name, st := range stats {
			if name == "lo" {
				continue
			}
			sum := st.rx + st.tx
			if sum > bestSum {
				bestSum = sum
				best = name
			}
		}
		if best == "" {
			for name, st := range stats {
				sum := st.rx + st.tx
				if sum > bestSum {
					bestSum = sum
					best = name
				}
			}
		}
		iface = best
	}
	st := stats[iface]
	return TrafficStats{
		Iface: iface,
		RXGB:  fmt.Sprintf("%.2f", float64(st.rx)/(1024*1024*1024)),
		TXGB:  fmt.Sprintf("%.2f", float64(st.tx)/(1024*1024*1024)),
	}, nil
}

func (m *Manager) DefaultLinks(serverLinks map[string]map[string]string) []string {
	hosts := make([]string, 0, len(serverLinks))
	for host := range serverLinks {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	var out []string
	for _, host := range hosts {
		if link := serverLinks[host]["default"]; link != "" {
			out = append(out, link)
		}
	}
	return out
}
