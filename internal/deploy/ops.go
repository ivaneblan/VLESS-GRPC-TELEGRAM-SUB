package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/links"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/xray"
)

func Vless(paths config.Paths, serverIDs []string, forceKeys, allowEmptyClients bool) error {
	cfg, sec, st, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	userUUIDs := config.CollectUUIDs(st)

	targets := serverIDs
	if len(targets) == 0 {
		for _, s := range cfg.Servers {
			targets = append(targets, s.ID)
		}
	}

	// Ensure every bridge has a relay UUID (its client id on the exit). Done up
	// front so exit deploys can include it in their client lists.
	for i := range cfg.Servers {
		s := cfg.Servers[i]
		if s.IsBridge() && strings.TrimSpace(sec.RelayUUID(s.ID)) == "" {
			sec.SetRelayUUID(s.ID, uuid.New().String())
		}
	}
	// Map each exit id -> relay UUIDs of bridges that forward to it.
	relayByExit := map[string][]string{}
	for i := range cfg.Servers {
		s := cfg.Servers[i]
		if s.IsBridge() {
			if ru := strings.TrimSpace(sec.RelayUUID(s.ID)); ru != "" {
				relayByExit[s.RelayTo] = append(relayByExit[s.RelayTo], ru)
			}
		}
	}

	// Deploy exits before bridges: a bridge needs its exit's Reality keys.
	targets = orderExitsFirst(cfg, targets)

	for i, id := range targets {
		server := cfg.ServerByID(id)
		if server == nil {
			return fmt.Errorf("unknown server id: %s", id)
		}
		role := "exit"
		if server.IsBridge() {
			role = "bridge -> " + server.RelayTo
		}
		step(i+1, len(targets), fmt.Sprintf("VLESS on %s (%s @ %s, %s)", server.Name, server.ID, server.Host, role))

		clientUUIDs := userUUIDs
		if !server.IsBridge() {
			if extra := relayByExit[server.ID]; len(extra) > 0 {
				clientUUIDs = append(append([]string{}, userUUIDs...), extra...)
			}
		}
		logf("%d client UUID(s) for this node", len(clientUUIDs))

		connectMsg(server.Host)
		client, err := sshclient.Connect(server.Host, sec, server.ID)
		if err != nil {
			return err
		}
		connectedMsg(server.Host)
		logf("install SSH authorized_key")
		status, err := sshclient.InstallAuthorizedKey(client, sec.SSH.PublicKey)
		if err != nil {
			client.Close()
			return err
		}
		if status == "already" {
			logf("authorized_key already present")
		} else {
			logOK("authorized_key added")
		}
		result, err := xray.Deploy(client, cfg, sec, server, clientUUIDs, forceKeys, allowEmptyClients)
		client.Close()
		if err != nil {
			return err
		}
		logOK("xray status: %s", result.Status)

		// Persist Reality keys / relay UUID now so a later failure in this loop
		// does not force regeneration on the next run.
		if err := config.SaveSecrets(paths.SecretsPath, sec); err != nil {
			return err
		}

		// Make sure the exit accepts this bridge's relay UUID even when the exit
		// itself was not part of this deploy run.
		if server.IsBridge() {
			if err := registerRelayOnExit(cfg, sec, server); err != nil {
				return err
			}
		}
	}
	if err := config.SaveSecrets(paths.SecretsPath, sec); err != nil {
		return err
	}
	logOK("secrets.yaml updated (Reality keys + relay UUIDs)")
	return nil
}

// orderExitsFirst returns target ids with exit servers before bridge servers,
// preserving relative order within each group.
func orderExitsFirst(cfg *config.Config, ids []string) []string {
	var exits, bridges []string
	for _, id := range ids {
		if s := cfg.ServerByID(id); s != nil && s.IsBridge() {
			bridges = append(bridges, id)
		} else {
			exits = append(exits, id)
		}
	}
	return append(exits, bridges...)
}

// registerRelayOnExit upserts a bridge's relay UUID into its exit's client list.
func registerRelayOnExit(cfg *config.Config, sec *config.Secrets, bridge *config.ServerDef) error {
	exit := cfg.ExitForBridge(bridge)
	if exit == nil {
		return fmt.Errorf("bridge %s: relay_to %q not found", bridge.ID, bridge.RelayTo)
	}
	relayUUID := strings.TrimSpace(sec.RelayUUID(bridge.ID))
	if relayUUID == "" {
		return fmt.Errorf("bridge %s: missing relay UUID", bridge.ID)
	}
	logf("register relay UUID on exit %s (%s)", exit.ID, exit.Host)
	client, err := sshclient.Connect(exit.Host, sec, exit.ID)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := xray.UpsertClient(client, relayUUID, cfg.Xray.Flow); err != nil {
		return err
	}
	logOK("relay UUID active on exit %s", exit.ID)
	return nil
}

func RefreshLinks(paths config.Paths) error {
	cfg, sec, st, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	updated := 0
	userIDs := make([]string, 0, len(st.Users))
	for userID := range st.Users {
		userIDs = append(userIDs, userID)
	}
	for ui, userID := range userIDs {
		info := st.Users[userID]
		uuid := info.UUID
		if uuid == "" {
			continue
		}
		step(ui+1, len(userIDs), fmt.Sprintf("links for user %s (%s)", userID, info.Label))
		serverLinks := map[string]map[string]string{}
		for _, server := range cfg.Servers {
			logf("server %s: upsert UUID in xray + build links", server.Name)
			params, err := links.ParamsFromConfig(cfg, sec, &server)
			if err != nil {
				return err
			}
			connectMsg(server.Host)
			client, err := sshclient.Connect(server.Host, sec, server.ID)
			if err != nil {
				return err
			}
			if err := xray.UpsertClient(client, uuid, cfg.Xray.Flow); err != nil {
				client.Close()
				return err
			}
			client.Close()
			serverLinks[server.Host] = links.BuildServerLinks(params, uuid)
			logOK("%s: links built", server.Name)
		}
		info.Servers = serverLinks
		st.Users[userID] = info
		updated++
	}
	if err := config.SaveState(paths.StatePath, st); err != nil {
		return err
	}
	logOK("state.yaml updated (%d users)", updated)
	return nil
}

func Backup(paths config.Paths) error {
	cfg, sec, st, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.BackupDir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	snapshot := map[string]string{
		"state":   paths.StatePath,
		"config":  paths.ConfigPath,
		"secrets": paths.SecretsPath,
	}
	for name, src := range snapshot {
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("backup: missing %s: %w", src, err)
		}
		backupPath := filepath.Join(paths.BackupDir, name+"-"+stamp+".yaml")
		latestPath := filepath.Join(paths.BackupDir, name+"-latest.yaml")
		if err := copyFile(src, backupPath); err != nil {
			return fmt.Errorf("backup %s: %w", name, err)
		}
		if err := copyFile(backupPath, latestPath); err != nil {
			return fmt.Errorf("backup %s latest: %w", name, err)
		}
		logx.Infof("backup: %s", backupPath)
	}

	metaPath := filepath.Join(paths.BackupDir, "snapshot-"+stamp+".txt")
	var meta strings.Builder
	meta.WriteString("snapshot: " + stamp + "\n")
	meta.WriteString("purpose: preserve users + server params for redeploy\n\n")
	for _, s := range cfg.Servers {
		r := sec.Reality(s.ID)
		keyStatus := "ok"
		if r.PublicKey == "" || r.PrivateKey == "" || r.ShortID == "" {
			keyStatus = "missing (will be generated on vless deploy; links refresh after)"
		}
		meta.WriteString(fmt.Sprintf("server %s (%s): reality %s\n", s.ID, s.Host, keyStatus))
	}
	meta.WriteString(fmt.Sprintf("\nusers: %d\n", len(st.Users)))
	for id, u := range st.Users {
		exp := "never"
		if !u.NeverExpires && u.ExpiresAt != nil {
			exp = fmt.Sprintf("%d", *u.ExpiresAt)
		}
		meta.WriteString(fmt.Sprintf("  - %s | %s | %s | expires: %s\n", id, u.Label, u.UUID, exp))
		fmt.Printf("  - %s | %s | %s | expires: %s\n", id, u.Label, u.UUID, exp)
	}
	if err := os.WriteFile(metaPath, []byte(meta.String()), 0o600); err != nil {
		return err
	}
	logx.Infof("snapshot: %s", metaPath)
	logx.Infof("users: %d", len(st.Users))
	return nil
}

const cleanupScript = `
set +e
echo "=== cleanup on $(hostname) ==="
echo "[1/4] stopping services..."
for svc in xray hysteria hysteria-server wg-quick@warp tg-subscription-bot; do
  echo "  stop $svc ..."
  timeout 30 systemctl stop "$svc" 2>/dev/null || true
  systemctl disable "$svc" 2>/dev/null || true
done
echo "  kill stray bot/hysteria processes..."
pkill -f '[t]gbot' 2>/dev/null || true
pkill -f '[b]ot.py' 2>/dev/null || true
pkill -f '[h]ysteria' 2>/dev/null || true
echo "[2/4] removing xray..."
if [ -x /usr/local/bin/xray ] || systemctl list-unit-files xray.service >/dev/null 2>&1; then
  timeout 30 systemctl stop xray 2>/dev/null || true
  systemctl disable xray 2>/dev/null || true
  rm -f /usr/local/bin/xray /etc/systemd/system/xray.service /etc/systemd/system/xray@.service
  rm -rf /usr/local/share/xray /etc/xray /var/log/xray /usr/local/etc/xray
  systemctl daemon-reload 2>/dev/null || true
fi
echo "[3/5] removing bot and legacy files..."
rm -rf /root/ssh /etc/hysteria /etc/wireguard/warp.conf /etc/wireguard/wgcf-account.toml \
  /etc/wireguard/wgcf-profile.conf /usr/local/bin/hysteria /usr/local/bin/wgcf /tmp/install_key_target.py
echo "[4/5] clearing root ~/.ssh (authorized_keys)..."
rm -rf /root/.ssh
echo "[5/5] daemon-reload..."
systemctl daemon-reload 2>/dev/null || true
echo "cleanup done"
`

func Cleanup(paths config.Paths) error {
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	var targets []config.ServerDef
	seen := map[string]bool{}
	for _, server := range cfg.Servers {
		if seen[server.Host] {
			continue
		}
		seen[server.Host] = true
		targets = append(targets, server)
	}
	for i, server := range targets {
		step(i+1, len(targets), fmt.Sprintf("cleanup %s (%s)", server.Name, server.Host))
		connectMsg(server.Host)
		client, err := sshclient.Connect(server.Host, sec, server.ID)
		if err != nil {
			return err
		}
		connectedMsg(server.Host)
		logf("remote cleanup script (live output):")
		rc, _, errStr := runScriptWithHeartbeat(client, cleanupScript, 300*time.Second, "cleanup")
		client.Close()
		if rc != 0 {
			return fmt.Errorf("cleanup %s: %s", server.Host, errStr)
		}
		logOK("cleanup finished on %s", server.Name)
	}
	logOK("all servers cleaned")
	return nil
}

func Keys(paths config.Paths) error {
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	pubkey := sec.SSH.PublicKey
	if pubkey == "" {
		return fmt.Errorf("empty SSH public key — run: vpnctl init")
	}
	bot := cfg.BotServer()
	for i, server := range cfg.Servers {
		host := server.Host
		step(i+1, len(cfg.Servers), fmt.Sprintf("SSH key on %s (%s)", server.Name, host))
		tryDirect := func() error {
			connectMsg(host)
			client, err := sshclient.Connect(host, sec, server.ID)
			if err != nil {
				return err
			}
			defer client.Close()
			connectedMsg(host)
			logf("write authorized_keys")
			status, err := sshclient.InstallAuthorizedKey(client, pubkey)
			if err != nil {
				return err
			}
			if status == "already" {
				logOK("key already on %s", host)
			} else {
				logOK("key installed on %s", host)
			}
			return nil
		}
		if err := tryDirect(); err != nil {
			if bot == nil || host == bot.Host {
				return err
			}
			logf("direct failed, trying via jump host %s", bot.Host)
			if err := installKeyViaJump(bot, sec, &server, pubkey); err != nil {
				return err
			}
		}
	}
	logOK("SSH keys on all servers")
	return nil
}

func installKeyViaJump(jump *config.ServerDef, sec *config.Secrets, target *config.ServerDef, pubkey string) error {
	jumpClient, err := sshclient.Connect(jump.Host, sec, jump.ID)
	if err != nil {
		return err
	}
	defer jumpClient.Close()
	targetPass := sec.Password(target.ID)
	escaped := pubkey
	script := fmt.Sprintf(`python3 - <<'PY'
import paramiko
HOST=%q
PASSWORD=%q
PUBKEY=%q
c=paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username="root", password=PASSWORD, timeout=10, look_for_keys=False, allow_agent=False)
cmd="mkdir -p ~/.ssh && chmod 700 ~/.ssh && grep -qxF '{k}' ~/.ssh/authorized_keys 2>/dev/null || echo '{k}' >> ~/.ssh/authorized_keys; chmod 600 ~/.ssh/authorized_keys".format(k=PUBKEY)
_, so, se = c.exec_command(cmd, timeout=10)
rc = so.channel.recv_exit_status()
print("ok" if rc == 0 else "fail")
c.close()
PY`, target.Host, targetPass, escaped)
	rc, out, errStr := jumpClient.RunScript(script, 60*time.Second)
	if rc != 0 || !contains(out, "ok") {
		return fmt.Errorf("%s via jump %s: %s %s", target.Host, jump.Host, out, errStr)
	}
	logOK("key installed via jump %s -> %s", jump.Host, target.Host)
	return nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func Health(paths config.Paths) error {
	cfg, sec, st, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	uuids := config.CollectUUIDs(st)
	var issues []string
	for i, server := range cfg.Servers {
		label := server.Name
		step(i+1, len(cfg.Servers), fmt.Sprintf("health check %s (%s)", label, server.Host))
		connectMsg(server.Host)
		client, err := sshclient.Connect(server.Host, sec, server.ID)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: ssh failed (%v)", label, err))
			logf("SSH failed: %v", err)
			continue
		}
		connectedMsg(server.Host)
		logf("check xray service")
		rc, out, _ := client.Run("systemctl is-active xray 2>/dev/null || echo no_xray", 30*time.Second)
		status := "unknown"
		if rc == 0 && out != "" {
			lines := splitLines(out)
			status = lines[len(lines)-1]
		}
		if status == "active" {
			logOK("%s: xray active", label)
		} else {
			issues = append(issues, fmt.Sprintf("%s: xray %s", label, status))
			logf("%s: xray %s", label, status)
		}
		if status == "active" && len(uuids) > 0 {
			logf("verify %d UUID(s) in xray config", len(uuids))
			check := buildUUIDCheck(uuids)
			rc, out, _ := client.RunScript(check, 30*time.Second)
			if rc == 0 && contains(out, "missing:") {
				issues = append(issues, fmt.Sprintf("%s: %s", label, trimLine(out)))
			} else if rc == 0 {
				logOK("all UUIDs present on %s", label)
			}
		}
		client.Close()
	}
	fmt.Println("\n=== ISSUES ===")
	if len(issues) == 0 {
		fmt.Println("none")
		return nil
	}
	for _, i := range issues {
		fmt.Println("-", i)
	}
	return fmt.Errorf("%d issue(s)", len(issues))
}

func buildUUIDCheck(uuids []string) string {
	raw, _ := json.Marshal(uuids)
	return fmt.Sprintf(`python3 - <<'PY'
import json
c = json.load(open('/usr/local/etc/xray/config.json'))
ids = {cl['id'] for ib in c.get('inbounds', []) if ib.get('protocol') == 'vless' for cl in ib.get('settings', {}).get('clients', [])}
exp = set(json.loads(%q))
missing = sorted(exp - ids)
print('ok' if not missing else 'missing:' + ','.join(missing))
PY`, string(raw))
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimLine(s string) string {
	lines := splitLines(s)
	if len(lines) == 0 {
		return s
	}
	return lines[len(lines)-1]
}

func All(paths config.Paths, forceKeys, skipBot bool) error {
	phase("[1/5] SSH keys")
	if err := Keys(paths); err != nil {
		return err
	}
	phase("[2/5] VLESS / Xray")
	if err := Vless(paths, nil, forceKeys, false); err != nil {
		return err
	}
	phase("[3/5] Subscription links")
	if err := RefreshLinks(paths); err != nil {
		return err
	}
	if skipBot {
		logf("skip Telegram bot (--no-bot)")
	} else {
		phase("[4/5] Telegram bot")
		if err := Bot(paths); err != nil {
			return err
		}
	}
	phase("[5/5] Health check")
	return Health(paths)
}

// Bootstrap installs infra on new VPS from scratch (no backup, empty state OK).
func Bootstrap(paths config.Paths, cleanupFirst, skipBot bool) error {
	phase("Bootstrap — fresh install (new servers)")
	copyIfMissing(paths.StatePath, filepath.Join(paths.Root, "state.example.yaml"))

	cfg, sec, st, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	config.SyncServerEntries(cfg, sec)
	if !skipBot && (sec.Telegram.BotToken == "" || strings.Contains(sec.Telegram.BotToken, "PASTE")) {
		return fmt.Errorf("fill telegram.bot_token in secrets.yaml before bootstrap (or use --no-bot)")
	}
	for _, s := range cfg.Servers {
		if strings.TrimSpace(sec.Password(s.ID)) == "" {
			return fmt.Errorf("fill servers.%s.password in secrets.yaml", s.ID)
		}
	}
	_ = config.SaveSecrets(paths.SecretsPath, sec)
	hasUsers := len(st.Users) > 0
	if hasUsers {
		logf("state has %d user(s) — links will be refreshed", len(st.Users))
	} else {
		logf("empty state.yaml — add users via: vpnctl users add <id>")
	}
	_ = cfg

	if cleanupFirst {
		phase("Cleanup before install")
		if err := Cleanup(paths); err != nil {
			return err
		}
	}

	phase("[1/4] SSH keys")
	if err := Keys(paths); err != nil {
		return err
	}
	phase("[2/4] VLESS / Xray")
	if err := Vless(paths, nil, false, true); err != nil {
		return err
	}
	stepN := 3
	total := 4
	if hasUsers {
		phase(fmt.Sprintf("[%d/5] Subscription links", stepN))
		if err := RefreshLinks(paths); err != nil {
			return err
		}
		stepN++
		total = 5
	} else {
		logf("skip links refresh (no users yet)")
	}
	if skipBot {
		logf("skip Telegram bot (--no-bot)")
	} else {
		phase(fmt.Sprintf("[%d/%d] Telegram bot", stepN, total))
		if err := Bot(paths); err != nil {
			return err
		}
		stepN++
	}
	phase(fmt.Sprintf("[%d/%d] Health check", stepN, total))
	if err := Health(paths); err != nil {
		return err
	}
	logOK("bootstrap complete")
	if !hasUsers {
		fmt.Println("\nNext: vpnctl users add <user_id>   (or use Telegram bot if deployed)")
	}
	return nil
}

func Redeploy(paths config.Paths, forceKeys, skipBot bool) error {
	phase("Pre-redeploy backup (state + config + secrets)")
	if err := Backup(paths); err != nil {
		return fmt.Errorf("redeploy aborted: backup failed: %w", err)
	}
	if forceKeys {
		return fmt.Errorf("redeploy with new Reality keys would break existing client links; run vless --new-keys only when intentional")
	}
	phase("Cleanup (all servers)")
	if err := Cleanup(paths); err != nil {
		return err
	}
	phase("Full deploy")
	return All(paths, forceKeys, skipBot)
}
