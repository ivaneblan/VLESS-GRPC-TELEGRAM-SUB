package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/botapp"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	paths := config.DefaultPaths(wd)
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if cfg.Bot.ApproverUserID == 0 {
		fmt.Fprintln(os.Stderr, "config: bot.approver_user_id is required")
		os.Exit(1)
	}
	token, err := botapp.ReadBotToken(sec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no bot token: set TG_BOT_TOKEN or secrets.yaml telegram.bot_token")
		os.Exit(1)
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("tgbot started, polling...")
	tg.Start(ctx)
}
