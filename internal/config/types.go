package config

type Config struct {
	Bot     BotConfig     `yaml:"bot"`
	Xray    XrayConfig    `yaml:"xray"`
	Servers []ServerDef   `yaml:"servers"`
}

type BotConfig struct {
	ServerID                string `yaml:"server_id"`
	ApproverUserID          int64  `yaml:"approver_user_id"`
	ApproverUsername        string `yaml:"approver_username,omitempty"`
	BroadcastOnlyApprover   bool   `yaml:"broadcast_only_approver"`
	DefaultSubscriptionDays int    `yaml:"default_subscription_days"`
}

type XrayConfig struct {
	Port            int    `yaml:"port"`
	SNI             string `yaml:"sni"`
	RealityDest     string `yaml:"reality_dest"`
	GRPCServiceName string `yaml:"grpc_service_name"`
	FPDesktop       string `yaml:"fp_desktop"`
	FPMobile        string `yaml:"fp_mobile"`
	Flow            string `yaml:"flow"`
	XHTTPPort       int    `yaml:"xhttp_port"`
	XHTTPPath       string `yaml:"xhttp_path"`
	XHTTPMode       string `yaml:"xhttp_mode"`
}

type ServerDef struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Host string `yaml:"host"`
}

type Secrets struct {
	Telegram TelegramSecrets       `yaml:"telegram"`
	SSH      SSHSecrets            `yaml:"ssh"`
	Servers  map[string]ServerSecret `yaml:"servers"`
}

type TelegramSecrets struct {
	BotToken string `yaml:"bot_token"`
}

type SSHSecrets struct {
	PrivateKey     string `yaml:"private_key,omitempty"`
	PublicKey      string `yaml:"public_key,omitempty"`
	PrivateKeyPath string `yaml:"private_key_path,omitempty"`
	PublicKeyPath  string `yaml:"public_key_path,omitempty"`
}

type ServerSecret struct {
	Password string       `yaml:"password"`
	Reality  RealityKeys  `yaml:"reality,omitempty"`
}

type RealityKeys struct {
	PrivateKey string `yaml:"private_key"`
	PublicKey  string `yaml:"public_key"`
	ShortID    string `yaml:"short_id"`
}

type State struct {
	ApproverChatID    *int64                 `yaml:"approver_chat_id"`
	LastExpirySweepAt int64                  `yaml:"last_expiry_sweep_at"`
	Requests          map[string]interface{} `yaml:"requests"`
	Users             map[string]UserEntry   `yaml:"users"`
}

type UserEntry struct {
	UUID         string                            `yaml:"uuid"`
	Label        string                            `yaml:"label"`
	CreatedAt    int64                             `yaml:"created_at"`
	ExpiresAt    *int64                            `yaml:"expires_at,omitempty"`
	NeverExpires bool                              `yaml:"never_expires,omitempty"`
	Servers      map[string]map[string]string      `yaml:"servers,omitempty"`
}

func (c *Config) ServerByID(id string) *ServerDef {
	for i := range c.Servers {
		if c.Servers[i].ID == id {
			return &c.Servers[i]
		}
	}
	return nil
}

func (c *Config) ServerByHost(host string) *ServerDef {
	for i := range c.Servers {
		if c.Servers[i].Host == host {
			return &c.Servers[i]
		}
	}
	return nil
}

func (c *Config) BotServer() *ServerDef {
	if c.Bot.ServerID != "" {
		if s := c.ServerByID(c.Bot.ServerID); s != nil {
			return s
		}
	}
	if len(c.Servers) > 0 {
		return &c.Servers[0]
	}
	return nil
}

func (c *Config) NonBotServers() []ServerDef {
	bot := c.BotServer()
	if bot == nil {
		return c.Servers
	}
	out := make([]ServerDef, 0, len(c.Servers))
	for _, s := range c.Servers {
		if s.ID != bot.ID {
			out = append(out, s)
		}
	}
	return out
}

func (s *Secrets) Password(serverID string) string {
	if s.Servers == nil {
		return ""
	}
	return s.Servers[serverID].Password
}

func (s *Secrets) Reality(serverID string) RealityKeys {
	if s.Servers == nil {
		return RealityKeys{}
	}
	return s.Servers[serverID].Reality
}

func (s *Secrets) SetReality(serverID string, keys RealityKeys) {
	if s.Servers == nil {
		s.Servers = map[string]ServerSecret{}
	}
	sec := s.Servers[serverID]
	sec.Reality = keys
	s.Servers[serverID] = sec
}

func (s *Secrets) SetPassword(serverID, password string) {
	if s.Servers == nil {
		s.Servers = map[string]ServerSecret{}
	}
	sec := s.Servers[serverID]
	sec.Password = password
	s.Servers[serverID] = sec
}

func (x XrayConfig) WithDefaults() XrayConfig {
	if x.Port == 0 {
		x.Port = 443
	}
	if x.SNI == "" {
		x.SNI = "cdn.ozon.ru"
	}
	if x.RealityDest == "" {
		x.RealityDest = "cdn.ozon.ru:443"
	}
	if x.GRPCServiceName == "" {
		x.GRPCServiceName = "GunService"
	}
	if x.FPDesktop == "" {
		x.FPDesktop = "safari"
	}
	if x.FPMobile == "" {
		x.FPMobile = "firefox"
	}
	if x.Flow == "" {
		x.Flow = "xtls-rprx-vision"
	}
	if x.XHTTPPort == 0 {
		x.XHTTPPort = 8444
	}
	if x.XHTTPPath == "" {
		x.XHTTPPath = "/api/v1/update"
	}
	if x.XHTTPMode == "" {
		x.XHTTPMode = "packet-up"
	}
	return x
}
