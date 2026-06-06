package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func runSSHKeygen(priv string) error {
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", priv, "-N", "", "-C", "vpnctl")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-keygen failed (install OpenSSH): %w", err)
	}
	return nil
}

func init() {
	_ = runtime.GOOS
}
