package botapp

import (
	"fmt"
	"os"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/subscription"
)

type Service struct {
	*subscription.Manager
}

func NewService(cfg *config.Config, sec *config.Secrets, paths config.Paths) *Service {
	return &Service{subscription.NewManager(cfg, sec, paths)}
}

func (s *Service) ApproverLabel() string {
	u := strings.TrimPrefix(strings.ToLower(s.Cfg.Bot.ApproverUsername), "@")
	if u != "" {
		return "@" + u
	}
	return "администратора"
}

func (s *Service) BuildHappCodeMessage(serverLinks map[string]map[string]string, expiresAt *int64) string {
	return s.BuildSubscriptionMessage(serverLinks, expiresAt)
}

func ReadBotToken(sec *config.Secrets) (string, error) {
	if t := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")); t != "" {
		return t, nil
	}
	t := strings.TrimSpace(sec.Telegram.BotToken)
	if t == "" {
		return "", fmt.Errorf("no bot token")
	}
	return t, nil
}
