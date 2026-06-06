package xray

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
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
	keys := sec.Reality(server.ID)
	if forceKeys || keys.PublicKey == "" || keys.PrivateKey == "" || keys.ShortID == "" {
		fmt.Println("  · generating Reality keys (xray x25519)...")
		var err error
		keys, err = generateRealityKeys(client)
		if err != nil {
			return nil, err
		}
		fmt.Println("  ✓ new Reality keys generated")
	} else {
		fmt.Printf("  · reusing Reality keys for %s\n", server.ID)
	}

	if err := ensureInstalled(client); err != nil {
		return nil, err
	}

	if len(uuids) == 0 {
		if allowEmptyClients {
			fmt.Println("  · no users yet — xray with empty client list")
		} else {
			fmt.Println("  · no users in state — generating placeholder UUID")
			id, err := genUUID(client)
			if err != nil {
				return nil, err
			}
			uuids = []string{id}
		}
	}

	fmt.Printf("  · writing xray config (%d client UUIDs)...\n", len(uuids))
	xrayCfg := buildConfig(uuids, x, keys.PrivateKey, keys.ShortID)
	changed, err := applyConfig(client, xrayCfg)
	if err != nil {
		return nil, err
	}

	fmt.Println("  · configuring firewall + systemd...")
	_, _, _ = client.Run("command -v ufw >/dev/null 2>&1 && ufw allow 443/tcp || true", 30*time.Second)
	_, _, _ = client.Run("systemctl daemon-reload", 30*time.Second)
	_, _, _ = client.Run("systemctl enable xray", 30*time.Second)

	if changed {
		fmt.Println("  · restarting xray (config changed)...")
		rc, out, errStr := client.Run("systemctl restart xray", 60*time.Second)
		if rc != 0 {
			return nil, fmt.Errorf("xray restart: %s %s", out, errStr)
		}
		fmt.Println("  ✓ xray config updated and restarted")
	} else {
		fmt.Println("  · xray config unchanged")
		_, out, _ := client.Run("systemctl is-active xray", 15*time.Second)
		if strings.TrimSpace(out) != "active" {
			fmt.Println("  · xray not active — restarting...")
			rc, out, errStr := client.Run("systemctl restart xray", 60*time.Second)
			if rc != 0 {
				return nil, fmt.Errorf("xray restart: %s %s", out, errStr)
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

func ensureInstalled(client *sshclient.Client) error {
	rc, out, _ := client.Run("command -v xray >/dev/null 2>&1 && xray version | head -1 || echo missing", 30*time.Second)
	if !strings.Contains(out, "missing") && strings.TrimSpace(out) != "" {
		fmt.Printf("  ✓ xray already installed: %s\n", strings.Split(strings.TrimSpace(out), "\n")[0])
		return nil
	}
	fmt.Println("  · installing xray (live output, 2–5 min)...")
	installScript := `set -e
echo "[xray] downloading install script..."
bash <(curl -fsSL https://raw.githubusercontent.com/XTLS/Xray-install/main/install-release.sh) install
echo "[xray] install finished"
`
	rc, out, errStr := client.RunScriptLive(installScript, 300*time.Second)
	if rc != 0 {
		return fmt.Errorf("xray install failed: %s %s", out, errStr)
	}
	fmt.Println("  ✓ xray installed")
	return nil
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

func applyConfig(client *sshclient.Client, cfg map[string]interface{}) (bool, error) {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
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
	rc, remoteOut, _ := client.Run(fmt.Sprintf("test -f %s && cat %s || echo", ConfigPath, ConfigPath), 30*time.Second)
	if rc != 0 || strings.TrimSpace(remoteOut) == "" {
		return true, nil
	}
	var remote map[string]interface{}
	if json.Unmarshal([]byte(remoteOut), &remote) != nil {
		return true, nil
	}
	return !jsonEqual(remote, cfg), nil
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
	rc, out, errStr = client.Run("systemctl restart xray", 60*time.Second)
	if rc != 0 {
		return fmt.Errorf("restart xray: %s %s", out, errStr)
	}
	return nil
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
	rc, out, errStr = client.Run("systemctl restart xray", 60*time.Second)
	if rc != 0 {
		return false, fmt.Errorf("restart: %s %s", out, errStr)
	}
	return true, nil
}
