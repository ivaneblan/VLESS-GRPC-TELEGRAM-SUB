package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
	"gopkg.in/yaml.v3"
)

const (
	remoteRoot     = "/root/ssh"
	remoteBotBin   = remoteRoot + "/tgbot"
	serviceName    = "tg-subscription-bot.service"
	servicePath    = "/etc/systemd/system/tg-subscription-bot.service"
	localBotLinux  = "dist/tgbot-linux-amd64"
)

func Bot(paths config.Paths) error {
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		return err
	}
	botServer := cfg.BotServer()
	if botServer == nil {
		return fmt.Errorf("bot server not configured")
	}
	logf("bot node: %s (%s @ %s)", botServer.Name, botServer.ID, botServer.Host)

	for _, s := range cfg.NonBotServers() {
		logf("stop bot on non-bot node %s", s.Name)
		if err := stopBotRemote(s, sec); err != nil {
			logf("warn: stop bot on %s: %v", s.Name, err)
		}
	}

	if err := ensureBotBinary(paths); err != nil {
		return err
	}

	connectMsg(botServer.Host)
	client, err := sshclient.Connect(botServer.Host, sec, botServer.ID)
	if err != nil {
		return err
	}
	defer client.Close()
	connectedMsg(botServer.Host)

	logf("upload bot binary to %s", remoteBotBin)
	if err := uploadBotBundle(client, paths); err != nil {
		return err
	}
	logf("stop old bot process")
	if err := stopBotService(client, false); err != nil {
		return err
	}
	logf("install systemd unit %s", servicePath)
	unit := renderSystemdUnit(cfg, paths)
	if err := sshclient.UploadBytes(client, servicePath, []byte(unit)); err != nil {
		return err
	}
	logf("systemctl daemon-reload + enable + restart")
	_, _, _ = client.Run("systemctl daemon-reload", 30*time.Second)
	_, _, _ = client.Run("systemctl enable "+serviceName, 30*time.Second)
	rc, out, errStr := client.Run("systemctl restart "+serviceName, 60*time.Second)
	if rc != 0 {
		return fmt.Errorf("systemctl restart: %s %s", out, errStr)
	}
	rc, status, _ := client.Run("systemctl is-active "+serviceName, 15*time.Second)
	logf("bot service: %s", strings.TrimSpace(status))
	if rc != 0 || strings.TrimSpace(status) != "active" {
		_, logOut, _ := client.Run("journalctl -u "+serviceName+" -n 30 --no-pager", 30*time.Second)
		return fmt.Errorf("bot not active:\n%s", logOut)
	}
	_, logOut, _ := client.Run("journalctl -u "+serviceName+" -n 15 --no-pager", 30*time.Second)
	fmt.Println(strings.TrimSpace(logOut))
	logOK("bot deploy finished")
	return nil
}

func ensureBotBinary(paths config.Paths) error {
	local := filepath.Join(paths.Root, filepath.FromSlash(localBotLinux))
	if _, err := os.Stat(local); err == nil {
		logOK("bot binary ready: %s", localBotLinux)
		return nil
	}
	logf("building %s (GOOS=linux GOARCH=amd64)...", localBotLinux)
	distDir := filepath.Join(paths.Root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", local, "./cmd/tgbot")
	cmd.Dir = paths.Root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build tgbot: %w\n%s", err, string(out))
	}
	logOK("built %s", localBotLinux)
	return nil
}

func uploadBotBundle(client *sshclient.Client, paths config.Paths) error {
	localBin := filepath.Join(paths.Root, filepath.FromSlash(localBotLinux))
	if err := sshclient.UploadFile(client, localBin, remoteBotBin); err != nil {
		return err
	}
	_, _, _ = client.Run("chmod +x "+remoteBotBin, 15*time.Second)
	logf("uploaded %s", localBotLinux)

	cfgData, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		return err
	}
	st, err := config.LoadState(paths.StatePath)
	if err != nil {
		return err
	}
	stData, err := yaml.Marshal(st)
	if err != nil {
		return err
	}
	sec, err := config.LoadSecrets(paths.SecretsPath, paths)
	if err != nil {
		return err
	}
	secData, err := config.MarshalSecrets(sec)
	if err != nil {
		return err
	}
	if err := sshclient.UploadBytes(client, remoteRoot+"/config.yaml", cfgData); err != nil {
		return err
	}
	if err := sshclient.UploadBytes(client, remoteRoot+"/secrets.yaml", secData); err != nil {
		return err
	}
	if err := sshclient.UploadBytes(client, remoteRoot+"/state.yaml", stData); err != nil {
		return err
	}
	logf("uploaded config.yaml, secrets.yaml, state.yaml")

	privKey := filepath.Join(paths.KeysDir, "id_ed25519")
	if _, err := os.Stat(privKey); err == nil {
		_ = sshclient.MkdirRemote(client, remoteRoot+"/keys")
		if err := sshclient.UploadFile(client, privKey, remoteRoot+"/keys/id_ed25519"); err != nil {
			return err
		}
		logf("uploaded keys/id_ed25519 (for bot SSH to exit nodes)")
	}
	return nil
}

func renderSystemdUnit(cfg *config.Config, paths config.Paths) string {
	tplPath := filepath.Join(paths.Root, "systemd", serviceName)
	data, err := os.ReadFile(tplPath)
	if err != nil {
		return fallbackUnit(cfg)
	}
	s := string(data)
	s = strings.Replace(s, "Environment=APPROVER_USER_ID=0", fmt.Sprintf("Environment=APPROVER_USER_ID=%d", cfg.Bot.ApproverUserID), 1)
	bcast := "0"
	if cfg.Bot.BroadcastOnlyApprover {
		bcast = "1"
	}
	s = strings.Replace(s, "Environment=BROADCAST_ONLY_APPROVER=1", "Environment=BROADCAST_ONLY_APPROVER="+bcast, 1)
	s = strings.Replace(s, "Environment=APPROVER_USERNAME=admin", "Environment=APPROVER_USERNAME="+cfg.Bot.ApproverUsername, 1)
	return s
}

func fallbackUnit(cfg *config.Config) string {
	bcast := "0"
	if cfg.Bot.BroadcastOnlyApprover {
		bcast = "1"
	}
	return fmt.Sprintf(`[Unit]
Description=Telegram VPN subscription bot
After=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
Environment=APPROVER_USER_ID=%d
Environment=BROADCAST_ONLY_APPROVER=%s
Environment=APPROVER_USERNAME=%s
ExecStart=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, remoteRoot, cfg.Bot.ApproverUserID, bcast, cfg.Bot.ApproverUsername, remoteBotBin)
}

func stopBotRemote(server config.ServerDef, sec *config.Secrets) error {
	client, err := sshclient.Connect(server.Host, sec, server.ID)
	if err != nil {
		return err
	}
	defer client.Close()
	return stopBotService(client, true)
}

func stopBotService(client *sshclient.Client, disable bool) error {
	_, _, _ = client.Run("pkill -f '[t]gbot' || true", 15*time.Second)
	_, _, _ = client.Run("pkill -f '[b]ot.py' || true", 15*time.Second)
	_, _, _ = client.Run("systemctl stop "+serviceName+" 2>/dev/null || true", 15*time.Second)
	if disable {
		_, _, _ = client.Run("systemctl disable "+serviceName+" 2>/dev/null || true", 15*time.Second)
	}
	_, out, _ := client.Run("systemctl show -p ActiveState --value "+serviceName+" 2>/dev/null || echo inactive", 15*time.Second)
	logf("bot service state: %s", strings.TrimSpace(out))
	return nil
}

// InitProject creates yaml templates and SSH keys.
func InitProject(paths config.Paths) error {
	if err := os.MkdirAll(paths.KeysDir, 0o700); err != nil {
		return err
	}
	copyIfMissing(paths.ConfigPath, filepath.Join(paths.Root, "config.example.yaml"))
	copyIfMissing(paths.SecretsPath, filepath.Join(paths.Root, "secrets.example.yaml"))
	copyIfMissing(paths.StatePath, filepath.Join(paths.Root, "state.example.yaml"))

	priv := filepath.Join(paths.KeysDir, "id_ed25519")
	if _, err := os.Stat(priv); os.IsNotExist(err) {
		if err := runSSHKeygen(priv); err != nil {
			return err
		}
		fmt.Println("generated SSH key:", priv)
	}

	sec, err := config.LoadSecrets(paths.SecretsPath, paths)
	if err != nil {
		sec = &config.Secrets{Servers: map[string]config.ServerSecret{}}
	}
	if cfg, err := config.LoadConfig(paths.ConfigPath); err == nil {
		config.SyncServerEntries(cfg, sec)
	}
	if err := config.SaveSecrets(paths.SecretsPath, sec); err != nil {
		return err
	}

	fmt.Println("\nFill in:")
	fmt.Println("  config.yaml  — servers (id, host), bot.approver_user_id")
	fmt.Println("  secrets.yaml — telegram.bot_token, servers.<id>.password")
	fmt.Println("\nThen: vpnctl bootstrap")
	return nil
}
