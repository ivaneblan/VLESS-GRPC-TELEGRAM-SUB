package config

import (
	"os"
	"path/filepath"
)

const (
	ConfigFile  = "config.yaml"
	SecretsFile = "secrets.yaml"
	StateFile   = "state.yaml"
)

type Paths struct {
	Root        string
	ConfigPath  string
	SecretsPath string
	StatePath   string
	BackupDir   string
	KeysDir     string
}

func DefaultPaths(root string) Paths {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			root = "."
		}
	}
	return Paths{
		Root:        root,
		ConfigPath:  filepath.Join(root, envOr("VPNCTL_CONFIG", ConfigFile)),
		SecretsPath: filepath.Join(root, envOr("VPNCTL_SECRETS", SecretsFile)),
		StatePath:   filepath.Join(root, envOr("VPNCTL_STATE", StateFile)),
		BackupDir:   filepath.Join(root, "backups"),
		KeysDir:     filepath.Join(root, "keys"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
