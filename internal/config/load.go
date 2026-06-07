package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("config: servers list is empty")
	}
	cfg.Xray = cfg.Xray.WithDefaults()
	if cfg.Bot.ServerID == "" {
		cfg.Bot.ServerID = cfg.Servers[0].ID
	}
	if cfg.BotServer() == nil {
		return nil, fmt.Errorf("config: bot.server_id %q not found", cfg.Bot.ServerID)
	}
	if cfg.Bot.DefaultSubscriptionDays == 0 {
		cfg.Bot.DefaultSubscriptionDays = 30
	}
	if strings.TrimSpace(cfg.SubscriptionTitle) == "" {
		cfg.SubscriptionTitle = "smknVPN"
	}
	return &cfg, nil
}

func LoadSecrets(path string, paths Paths) (*Secrets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sec Secrets
	if err := yaml.Unmarshal(data, &sec); err != nil {
		return nil, err
	}
	if err := sec.resolveSSHKeys(paths); err != nil {
		return nil, err
	}
	return &sec, nil
}

func (s *Secrets) resolveSSHKeys(paths Paths) error {
	privPath := s.SSH.PrivateKeyPath
	if privPath == "" {
		privPath = "keys/id_ed25519"
	}
	if !filepathIsAbs(privPath) {
		privPath = joinPath(paths.Root, privPath)
	}
	pubPath := s.SSH.PublicKeyPath
	if pubPath == "" {
		pubPath = privPath + ".pub"
	}
	if !filepathIsAbs(pubPath) {
		pubPath = joinPath(paths.Root, pubPath)
	}
	if data, err := os.ReadFile(privPath); err == nil {
		s.SSH.PrivateKey = string(data)
	}
	if data, err := os.ReadFile(pubPath); err == nil {
		s.SSH.PublicKey = strings.TrimSpace(string(data))
	}
	return nil
}

// ForStorage returns a copy safe to write to secrets.yaml (no inline SSH keys).
func (s *Secrets) ForStorage() Secrets {
	out := *s
	out.SSH.PrivateKey = ""
	out.SSH.PublicKey = ""
	if strings.TrimSpace(out.SSH.PrivateKeyPath) == "" {
		out.SSH.PrivateKeyPath = "keys/id_ed25519"
	}
	if strings.TrimSpace(out.SSH.PublicKeyPath) == "" {
		out.SSH.PublicKeyPath = "keys/id_ed25519.pub"
	}
	if out.Servers == nil {
		return out
	}
	servers := make(map[string]ServerSecret, len(out.Servers))
	for id, srv := range out.Servers {
		if srv.Reality.PrivateKey == "" && srv.Reality.PublicKey == "" && srv.Reality.ShortID == "" {
			srv.Reality = RealityKeys{}
		}
		servers[id] = srv
	}
	out.Servers = servers
	return out
}

// SyncServerEntries ensures secrets.yaml has a password entry per config server id.
func SyncServerEntries(cfg *Config, sec *Secrets) {
	if sec.Servers == nil {
		sec.Servers = map[string]ServerSecret{}
	}
	for _, s := range cfg.Servers {
		if _, ok := sec.Servers[s.ID]; !ok {
			sec.Servers[s.ID] = ServerSecret{}
		}
	}
}

func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return nil, err
	}
	return ParseState(data)
}

func ParseState(data []byte) (*State, error) {
	var st State
	if err := yaml.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Requests == nil {
		st.Requests = map[string]interface{}{}
	}
	if st.Users == nil {
		st.Users = map[string]UserEntry{}
	}
	return &st, nil
}

func emptyState() *State {
	return &State{
		Requests: map[string]interface{}{},
		Users:    map[string]UserEntry{},
	}
}

func SaveConfig(path string, cfg *Config) error {
	return writeYAML(path, cfg)
}

func SaveSecrets(path string, sec *Secrets) error {
	stored := sec.ForStorage()
	return writeYAML(path, &stored)
}

func MarshalSecrets(sec *Secrets) ([]byte, error) {
	stored := sec.ForStorage()
	return yaml.Marshal(&stored)
}

func SaveState(path string, st *State) error {
	return writeYAML(path, st)
}

func writeYAML(path string, v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func joinPath(root, rel string) string {
	if strings.HasPrefix(rel, "/") || (len(rel) > 2 && rel[1] == ':') {
		return rel
	}
	sep := string(os.PathSeparator)
	if strings.HasSuffix(root, sep) {
		return root + rel
	}
	return root + sep + rel
}

func filepathIsAbs(path string) bool {
	return strings.HasPrefix(path, "/") || (len(path) > 2 && path[1] == ':')
}

func LoadAll(paths Paths) (*Config, *Secrets, *State, error) {
	cfg, err := LoadConfig(paths.ConfigPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	sec, err := LoadSecrets(paths.SecretsPath, paths)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("secrets: %w", err)
	}
	st, err := LoadState(paths.StatePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("state: %w", err)
	}
	return cfg, sec, st, nil
}

func CollectUUIDs(st *State) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range st.Users {
		id := strings.TrimSpace(u.UUID)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
