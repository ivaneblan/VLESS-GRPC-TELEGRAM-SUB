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
	tg.Start(ctx)
}
