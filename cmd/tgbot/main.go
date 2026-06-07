package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/botapp"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
)

func main() {
	logx.Setup(true, os.Getenv("DEBUG") != "")

	wd, err := os.Getwd()
	if err != nil {
		logx.L.Fatal().Err(err).Msg("getwd")
	}
	paths := config.DefaultPaths(wd)
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		logx.L.Fatal().Err(err).Msg("load config")
	}
	if cfg.Bot.ApproverUserID == 0 {
		logx.L.Fatal().Msg("config: bot.approver_user_id is required")
	}
	sshclient.StrictHostKey = cfg.SSH.StrictHostKey
	token, err := botapp.ReadBotToken(sec)
	if err != nil {
		logx.L.Fatal().Msg("no bot token: set TG_BOT_TOKEN or secrets.yaml telegram.bot_token")
	}

	app := botapp.NewApp(cfg, sec, paths)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			app.HandleUpdate(ctx, b, update)
		}),
	}

	tg, err := bot.New(token, opts...)
	if err != nil {
		logx.L.Fatal().Err(err).Msg("create bot")
	}
	logx.L.Info().Msg("tgbot started, polling...")
	// Start blocks until ctx is cancelled (SIGINT/SIGTERM via NotifyContext).
	tg.Start(ctx)
	// Graceful shutdown: state.yaml is always written synchronously under a lock
	// during each update, so there is nothing to flush here; per-operation SSH
	// connections are already closed via defer. Just stop signal handling and log.
	cancel()
	logx.L.Info().Msg("tgbot stopped")
}
