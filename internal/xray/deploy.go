package xray

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
)

const ConfigPath = "/usr/local/etc/xray/config.json"

type DeployResult struct {
	Status   string
	PublicKey string
	PrivateKey string
	ShortID  string
}

func Deploy(client *sshclient.Client, cfg *config.Config, sec *config.Secrets, server *config.ServerDef, uuids []string, forceKeys, allowEmptyClients bool) (*DeployResult, error) {
	x := cfg.Xray.WithDefaults()

	// xray must exist before we can run `xray x25519` / `xray uuid` below.
	freshInstall, err := ensureInstalled(client, x.Version)
	if err != nil {
		return nil, err
	}

	keys := sec.Reality(server.ID)
	if forceKeys || keys.PublicKey == "" || keys.PrivateKey == "" || keys.ShortID == "" {
		logx.Infof("generating Reality keys (xray x25519)...")
		var err error
		keys, err = generateRealityKeys(client)
		if err != nil {
			return nil, err
		}
		logx.Infof("new Reality keys generated")
	} else {
		logx.Infof("reusing Reality keys for %s", server.ID)
	}

	if len(uuids) == 0 {
		if allowEmptyClients {
			logx.Infof("no users yet — xray with empty client list")
		} else {
			logx.Infof("no users in state — generating placeholder UUID")
			id, err := genUUID(client)
			if err != nil {
				return nil, err
			}
			uuids = []string{id}
		}
	}

	logx.Infof("writing xray config (%d client UUIDs)...", len(uuids))
	var xrayCfg map[string]interface{}
	if server.IsBridge() {
		exit := cfg.ExitForBridge(server)
		if exit == nil {
			return nil, fmt.Errorf("bridge %s: relay_to %q not found in servers", server.ID, server.RelayTo)
		}
		if exit.IsBridge() {
			return nil, fmt.Errorf("bridge %s cannot relay to another bridge (%s)", server.ID, exit.ID)
		}
		exitKeys := sec.Reality(exit.ID)
		if exitKeys.PublicKey == "" || exitKeys.ShortID == "" {
			return nil, fmt.Errorf("exit %s has no Reality keys yet — deploy it first (vpnctl vless %s)", exit.ID, exit.ID)
		}
		relayUUID := strings.TrimSpace(sec.RelayUUID(server.ID))
		if relayUUID == "" {
			logx.Infof("generating relay UUID (bridge -> exit)...")
			id, err := genUUID(client)
			if err != nil {
				return nil, err
			}
			relayUUID = id
			sec.SetRelayUUID(server.ID, relayUUID)
		}
		logx.Infof("bridge mode: forwarding to exit %s (%s:%d) via VLESS", exit.ID, exit.Host, x.Port)
		xrayCfg = buildRelayConfig(uuids, x, keys.PrivateKey, keys.ShortID, exit.Host, x.Port, exitKeys.PublicKey, exitKeys.ShortID, relayUUID)
	} else {
		xrayCfg = buildConfig(uuids, x, keys.PrivateKey, keys.ShortID)
	}
	changed, err := applyConfig(client, xrayCfg)
	if err != nil {
		return nil, err
	}

	logx.Infof("configuring firewall + systemd...")
	_, _, _ = client.Run("command -v ufw >/dev/null 2>&1 && ufw allow 443/tcp || true", 30*time.Second)
	_, _, _ = client.Run("systemctl daemon-reload", 30*time.Second)
	_, _, _ = client.Run("systemctl enable xray", 30*time.Second)

	if changed || freshInstall {
		if freshInstall {
			logx.Infof("restarting xray (fresh install)...")
		} else {
			logx.Infof("restarting xray (config changed)...")
		}
		if err := restartXray(client); err != nil {
			return nil, err
		}
		logx.Infof("xray config updated and restarted")
	} else {
		logx.Infof("xray config unchanged")
		_, out, _ := client.Run("systemctl is-active xray", 15*time.Second)
		if strings.TrimSpace(out) != "active" {
			logx.Infof("xray not active — restarting...")
			if err := restartXray(client); err != nil {
				return nil, err
			}
		}
	}

	rc, out, _ := client.Run("systemctl is-active xray", 15*time.Second)
	status := strings.TrimSpace(out)
	if rc != 0 {
		status = "unknown"
	}

	sec.SetReality(server.ID, keys)
	return &DeployResult{
		Status:     status,
		PublicKey:  keys.PublicKey,
		PrivateKey: keys.PrivateKey,
		ShortID:    keys.ShortID,
	}, nil
}

// restartXray restarts the service and confirms it is active. Some hosts close
// the SSH exec channel during a service restart without sending an exit status
// (golang ssh returns ExitMissingError / non-zero rc), even though the restart
// succeeded; we therefore verify via `systemctl is-active` before failing.
func restartXray(client *sshclient.Client) error {
	rc, out, errStr := client.Run("systemctl restart xray", 60*time.Second)
	if rc == 0 {
		return nil
	}
	// The restart frequently drops our own SSH session when we are managing an
	// exit whose VPN carries this very connection. Reconnect, then verify.
	for i := 0; i < 5; i++ {
		time.Sleep(3 * time.Second)
		if err := client.Reconnect(); err != nil {
			continue
		}
		if _, active, _ := client.Run("systemctl is-active xray", 15*time.Second); strings.TrimSpace(active) == "active" {
			return nil
		}
	}
	return fmt.Errorf("restart xray: %s %s", out, errStr)
}

// ensureInstalled installs xray if missing. It returns true when a fresh
// install was performed (the service then runs the installer's default config,
// so the caller must restart after writing the real config).
//
// version pins the xray-core release to install. When empty, the latest
// release is installed and an already-installed xray is left untouched. When
// set, xray is installed/reinstalled to that exact version unless it already
// matches the installed one.
func ensureInstalled(client *sshclient.Client, version string) (bool, error) {
	version = strings.TrimSpace(version)
	rc, out, _ := client.Run("command -v xray >/dev/null 2>&1 && xray version | head -1 || echo missing", 30*time.Second)
	installed := !strings.Contains(out, "missing") && strings.TrimSpace(out) != ""
	if installed {
		current := strings.Split(strings.TrimSpace(out), "\n")[0]
		if version == "" || strings.Contains(current, version) {
			logx.Infof("xray already installed: %s", current)
			return false, nil
		}
		logx.Infof("xray %s installed, switching to pinned %s...", current, version)
	}

	versionArg := ""
	if version != "" {
		versionArg = " --version " + version
		logx.Infof("installing xray %s (live output, 2–5 min)...", version)
	} else {
		logx.Infof("installing xray latest (live output, 2–5 min)...")
	}
	installScript := fmt.Sprintf(`set -e
echo "[xray] preparing dependencies (unzip, curl)..."
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -y || true
  apt-get install -y unzip curl ca-certificates || true
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y unzip curl ca-certificates || true
elif command -v yum >/dev/null 2>&1; then
  yum install -y unzip curl ca-certificates || true
fi
echo "[xray] downloading install script..."
bash <(curl -fsSL https://raw.githubusercontent.com/XTLS/Xray-install/main/install-release.sh) install%s
echo "[xray] install finished"
`, versionArg)
	rc, out, errStr := client.RunScriptLive(installScript, 300*time.Second)
	if rc != 0 {
		return false, fmt.Errorf("xray install failed: %s %s", out, errStr)
	}
	logx.Infof("xray installed")
	return true, nil
}

var (
	privRe  = regexp.MustCompile(`Private(?: key|Key):\s*([^\r\n]+)`)
	pubRe   = regexp.MustCompile(`(?:Public key|Password \(PublicKey\)):\s*([^\r\n]+)`)
)

func generateRealityKeys(client *sshclient.Client) (config.RealityKeys, error) {
	rc, out, errStr := client.Run("xray x25519", 30*time.Second)
	if rc != 0 {
		return config.RealityKeys{}, fmt.Errorf("x25519: %s %s", out, errStr)
	}
	blob := out + errStr
	pm := privRe.FindStringSubmatch(blob)
	pubm := pubRe.FindStringSubmatch(blob)
	if pm == nil || pubm == nil {
		return config.RealityKeys{}, fmt.Errorf("parse x25519 output: %s", blob)
	}
	rc, out, errStr = client.Run("openssl rand -hex 8", 15*time.Second)
	if rc != 0 {
		return config.RealityKeys{}, fmt.Errorf("shortId: %s %s", out, errStr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	shortID := strings.TrimSpace(lines[len(lines)-1])
	return config.RealityKeys{
		PrivateKey: strings.TrimSpace(pm[1]),
		PublicKey:  strings.TrimSpace(pubm[1]),
		ShortID:    shortID,
	}, nil
}

func genUUID(client *sshclient.Client) (string, error) {
	rc, out, errStr := client.Run("xray uuid", 15*time.Second)
	if rc != 0 {
		return "", fmt.Errorf("uuid: %s %s", out, errStr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

func buildConfig(uuids []string, x config.XrayConfig, privateKey, shortID string) map[string]interface{} {
	clients := make([]map[string]string, 0, len(uuids))
	for _, id := range uuids {
		clients = append(clients, map[string]string{"id": id})
	}
	return map[string]interface{}{
		"log": map[string]string{"loglevel": "warning"},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "vless-grpc-reality",
				"listen":   "0.0.0.0",
				"port":     x.Port,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients":    clients,
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network": "grpc",
					"security": "reality",
					"grpcSettings": map[string]string{
						"serviceName": x.GRPCServiceName,
					},
					"realitySettings": map[string]interface{}{
						"show":        false,
						"dest":        x.RealityDest,
						"xver":        0,
						"serverNames": []string{x.SNI},
						"privateKey":  privateKey,
						"shortIds":    []string{shortID},
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{"protocol": "freedom"},
			{"protocol": "blackhole", "tag": "blocked"},
		},
	}
}

// buildRelayConfig builds the xray config for a bridge node: a normal
// VLESS+Reality+gRPC inbound (its own keys, the same client UUIDs) plus a VLESS
// outbound that dials the exit over gRPC+Reality using relayUUID. The relay
// outbound is listed first, so all client traffic is forwarded to the exit.
func buildRelayConfig(uuids []string, x config.XrayConfig, privateKey, shortID, exitHost string, exitPort int, exitPublicKey, exitShortID, relayUUID string) map[string]interface{} {
	clients := make([]map[string]string, 0, len(uuids))
	for _, id := range uuids {
		clients = append(clients, map[string]string{"id": id})
	}
	return map[string]interface{}{
		"log": map[string]string{"loglevel": "warning"},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "vless-grpc-reality",
				"listen":   "0.0.0.0",
				"port":     x.Port,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients":    clients,
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "grpc",
					"security": "reality",
					"grpcSettings": map[string]string{
						"serviceName": x.GRPCServiceName,
					},
					"realitySettings": map[string]interface{}{
						"show":        false,
						"dest":        x.RealityDest,
						"xver":        0,
						"serverNames": []string{x.SNI},
						"privateKey":  privateKey,
						"shortIds":    []string{shortID},
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "vless",
				"tag":      "relay",
				"settings": map[string]interface{}{
					"vnext": []map[string]interface{}{
						{
							"address": exitHost,
							"port":    exitPort,
							"users": []map[string]interface{}{
								{"id": relayUUID, "encryption": "none"},
							},
						},
					},
				},
				"streamSettings": map[string]interface{}{
					"network":  "grpc",
					"security": "reality",
					"grpcSettings": map[string]string{
						"serviceName": x.GRPCServiceName,
					},
					"realitySettings": map[string]interface{}{
						"serverName":  x.SNI,
						"publicKey":   exitPublicKey,
						"shortId":     exitShortID,
						"fingerprint": x.FPDesktop,
					},
				},
			},
			{"protocol": "freedom", "tag": "direct"},
			{"protocol": "blackhole", "tag": "blocked"},
		},
	}
}

func applyConfig(client *sshclient.Client, cfg map[string]interface{}) (bool, error) {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	// Detect change against the CURRENT remote config before overwriting it.
	changed := true
	_, existing, _ := client.Run(fmt.Sprintf("test -f %s && cat %s || echo", ConfigPath, ConfigPath), 30*time.Second)
	if strings.TrimSpace(existing) != "" {
		var remote map[string]interface{}
		if json.Unmarshal([]byte(existing), &remote) == nil {
			changed = !jsonEqual(remote, cfg)
		}
	}
	hex := fmt.Sprintf("%x", raw)
	writePy := fmt.Sprintf(
		"python3 -c \"from pathlib import Path; Path('%s').write_bytes(bytes.fromhex('%s'))\"",
		ConfigPath, hex,
	)
	rc, out, errStr := client.Run(writePy, 60*time.Second)
	if rc != 0 {
		return false, fmt.Errorf("write config: %s %s", out, errStr)
	}
	return changed, nil
}

func jsonEqual(a, b map[string]interface{}) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func UpsertClient(client *sshclient.Client, uuid string, flow string) error {
	rc, out, errStr := client.Run("cat "+ConfigPath, 30*time.Second)
	if rc != 0 {
		return fmt.Errorf("read xray config: %s %s", out, errStr)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return err
	}
	inbounds, _ := cfg["inbounds"].([]interface{})
	changed := false
	for _, ib := range inbounds {
		inbound, _ := ib.(map[string]interface{})
		if inbound["protocol"] != "vless" {
			continue
		}
		settings, _ := inbound["settings"].(map[string]interface{})
		clientsRaw, _ := settings["clients"].([]interface{})
		for _, c := range clientsRaw {
			cm, _ := c.(map[string]interface{})
			if cm["id"] == uuid {
				return nil
			}
		}
		entry := map[string]string{"id": uuid}
		settings["clients"] = append(clientsRaw, entry)
		changed = true
	}
	if !changed {
		return nil
	}
	_, err := applyConfig(client, cfg)
	if err != nil {
		return err
	}
	return restartXray(client)
}

func RemoveClient(client *sshclient.Client, uuid string) (bool, error) {
	rc, out, errStr := client.Run("cat "+ConfigPath, 30*time.Second)
	if rc != 0 {
		return false, fmt.Errorf("read config: %s %s", out, errStr)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return false, err
	}
	removed := false
	inbounds, _ := cfg["inbounds"].([]interface{})
	for _, ib := range inbounds {
		inbound, _ := ib.(map[string]interface{})
		if inbound["protocol"] != "vless" {
			continue
		}
		settings, _ := inbound["settings"].(map[string]interface{})
		clientsRaw, _ := settings["clients"].([]interface{})
		newClients := make([]interface{}, 0, len(clientsRaw))
		for _, c := range clientsRaw {
			cm, _ := c.(map[string]interface{})
			if cm["id"] == uuid {
				removed = true
				continue
			}
			newClients = append(newClients, c)
		}
		settings["clients"] = newClients
	}
	if !removed {
		return false, nil
	}
	_, err := applyConfig(client, cfg)
	if err != nil {
		return false, err
	}
	if err := restartXray(client); err != nil {
		return false, err
	}
	return true, nil
}
