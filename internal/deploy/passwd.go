package deploy

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
)

const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func RotateRootPassword(paths config.Paths, serverIDs []string, newPassword string, generate bool, length int) error {
	phase("Rotate root password")
	if err := Backup(paths); err != nil {
		return fmt.Errorf("backup before password change failed: %w", err)
	}

	cfg, err := config.LoadConfig(paths.ConfigPath)
	if err != nil {
		return err
	}
	sec, err := config.LoadSecrets(paths.SecretsPath, paths)
	if err != nil {
		return err
	}
	pubkey := strings.TrimSpace(sec.SSH.PublicKey)
	if pubkey == "" {
		return fmt.Errorf("no SSH public key — run: vpnctl init && vpnctl keys")
	}

	targets := serverIDs
	if len(targets) == 0 {
		for _, s := range cfg.Servers {
			targets = append(targets, s.ID)
		}
	}

	for i, id := range targets {
		server := cfg.ServerByID(id)
		if server == nil {
			return fmt.Errorf("unknown server id: %s", id)
		}
		pass := newPassword
		if generate || pass == "" {
			var err error
			pass, err = randomPassword(length)
			if err != nil {
				return err
			}
		}

		step(i+1, len(targets), fmt.Sprintf("password %s (%s)", server.Name, server.Host))
		oldPass := sec.Password(id)

		connectMsg(server.Host)
		client, err := sshclient.Connect(server.Host, sec, id)
		if err != nil {
			return fmt.Errorf("%s: connect failed: %w", server.Host, err)
		}
		connectedMsg(server.Host)

		logf("ensure SSH key is installed (fallback if password change fails)")
		keyStatus, err := sshclient.InstallAuthorizedKey(client, pubkey)
		if err != nil {
			client.Close()
			return fmt.Errorf("%s: install key: %w", server.Host, err)
		}
		logf("SSH key: %s", keyStatus)

		logf("set new root password on server")
		if err := sshclient.SetRootPassword(client, pass); err != nil {
			client.Close()
			return fmt.Errorf("%s: %w", server.Host, err)
		}

		logf("verify login with new password")
		if err := verifyRootPassword(server.Host, pass); err != nil {
			logf("new password rejected — reverting to previous password")
			if oldPass != "" {
				if revErr := sshclient.SetRootPassword(client, oldPass); revErr != nil {
					client.Close()
					return fmt.Errorf("%s: verify failed and revert failed — check SSH key access: %v (revert: %v)", server.Host, err, revErr)
				}
				if revVerify := verifyRootPassword(server.Host, oldPass); revVerify != nil {
					client.Close()
					return fmt.Errorf("%s: verify failed; revert may have failed — use SSH key: %v", server.Host, err)
				}
				client.Close()
				return fmt.Errorf("%s: new password verification failed; old password restored", server.Host)
			}
			client.Close()
			return fmt.Errorf("%s: new password verification failed: %w", server.Host, err)
		}

		logf("verify SSH key still works")
		client.Close()
		if _, err := sshclient.Connect(server.Host, sec, id); err != nil {
			return fmt.Errorf("%s: SSH key login failed after password change: %w", server.Host, err)
		}
		logOK("SSH key login OK")

		sec.SetPassword(id, pass)
		if err := config.SaveSecrets(paths.SecretsPath, sec); err != nil {
			return fmt.Errorf("save secrets.yaml: %w", err)
		}
		logOK("secrets.yaml updated for %s", id)
		if generate || newPassword == "" {
			fmt.Printf("  new password for %s (%s): %s\n", id, server.Host, pass)
		}
	}

	logOK("root password rotation complete")
	return nil
}

func verifyRootPassword(host, password string) error {
	client, err := sshclient.ConnectPassword(host, password)
	if err != nil {
		return err
	}
	client.Close()
	return nil
}

func randomPassword(length int) (string, error) {
	if length < 16 {
		length = 20
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}
