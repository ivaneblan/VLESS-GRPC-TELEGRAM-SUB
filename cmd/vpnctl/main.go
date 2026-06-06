package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/deploy"
	"github.com/spf13/cobra"
)

func main() {
	rootDir, _ := os.Getwd()
	paths := config.DefaultPaths(rootDir)

	root := &cobra.Command{
		Use:   "vpnctl",
		Short: "VLESS gRPC VPN deploy CLI",
	}
	root.PersistentFlags().StringVar(&paths.Root, "root", paths.Root, "project root directory")

	root.AddCommand(
		&cobra.Command{
			Use:   "init",
			Short: "Create YAML templates and SSH keys",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.InitProject(paths)
			},
		},
		&cobra.Command{
			Use:   "keys",
			Short: "Install SSH public key on all servers",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.Keys(paths)
			},
		},
		&cobra.Command{
			Use:   "cleanup",
			Short: "Remove xray, bot, legacy services from all servers",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.Cleanup(paths)
			},
		},
		vlessCmd(&paths),
		&cobra.Command{
			Use:   "bot",
			Short: "Deploy Telegram bot (tgbot) via systemd",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.Bot(paths)
			},
		},
		&cobra.Command{
			Use:   "check",
			Short: "Health check all exit servers",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.Health(paths)
			},
		},
		&cobra.Command{
			Use:   "backup",
			Short: "Backup state.yaml, config.yaml, secrets.yaml to backups/",
			RunE: func(cmd *cobra.Command, args []string) error {
				paths = config.DefaultPaths(paths.Root)
				return deploy.Backup(paths)
			},
		},
		allCmd(&paths),
		redeployCmd(&paths),
		bootstrapCmd(&paths),
		passwdCmd(&paths),
		linksCmd(&paths),
		usersCmd(&paths),
		serversCmd(&paths),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func allCmd(paths *config.Paths) *cobra.Command {
	var skipBot bool
	c := &cobra.Command{
		Use:   "all",
		Short: "keys + vless + links + bot + check",
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.All(*paths, false, skipBot)
		},
	}
	c.Flags().BoolVar(&skipBot, "no-bot", false, "skip Telegram bot deploy")
	return c
}

func redeployCmd(paths *config.Paths) *cobra.Command {
	var skipBot bool
	c := &cobra.Command{
		Use:   "redeploy",
		Short: "backup + cleanup + full deploy",
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Redeploy(*paths, false, skipBot)
		},
	}
	c.Flags().BoolVar(&skipBot, "no-bot", false, "skip Telegram bot deploy")
	return c
}

func passwdCmd(paths *config.Paths) *cobra.Command {
	var (
		password string
		generate bool
		length   int
	)
	c := &cobra.Command{
		Use:   "passwd [server-id...]",
		Short: "Change root password and update secrets.yaml",
		Long: `Safely rotates root password on VPS nodes.

  1. Backs up secrets.yaml
  2. Ensures SSH public key is in authorized_keys (recovery path)
  3. Sets new password on the server
  4. Verifies password login before saving
  5. Reverts on failure; updates secrets.yaml only after success

Examples:
  vpnctl passwd --generate          # all servers, random password
  vpnctl passwd de --password '...' # one server, explicit password`,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.RotateRootPassword(*paths, args, password, generate, length)
		},
	}
	c.Flags().StringVar(&password, "password", "", "new root password (all targeted servers)")
	c.Flags().BoolVar(&generate, "generate", false, "generate a random password per server")
	c.Flags().IntVar(&length, "length", 20, "generated password length (with --generate)")
	return c
}

func bootstrapCmd(paths *config.Paths) *cobra.Command {
	var cleanupFirst, skipBot bool
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Fresh install on new servers (empty state, no backup)",
		Long: `First-time deploy for new VPS nodes.

  1. vpnctl init
  2. Edit config.yaml and secrets.yaml (servers, passwords; bot token if using Telegram)
  3. vpnctl bootstrap [--no-bot]

Skips backup. Empty state.yaml is OK — add users with: vpnctl users add <id>
Use redeploy when reinstalling with existing subscribers.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Bootstrap(*paths, cleanupFirst, skipBot)
		},
	}
	c.Flags().BoolVar(&cleanupFirst, "cleanup", false, "remove old xray/bot on servers before install")
	c.Flags().BoolVar(&skipBot, "no-bot", false, "skip Telegram bot deploy (CLI-only user management)")
	return c
}

func vlessCmd(paths *config.Paths) *cobra.Command {
	var newKeys bool
	c := &cobra.Command{
		Use:   "vless [server-id...]",
		Short: "Deploy VLESS+gRPC Reality on servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Vless(*paths, args, newKeys, false)
		},
	}
	c.Flags().BoolVar(&newKeys, "new-keys", false, "regenerate Reality keys")
	return c
}

func linksCmd(paths *config.Paths) *cobra.Command {
	links := &cobra.Command{Use: "links", Short: "Manage subscription links"}
	links.AddCommand(&cobra.Command{
		Use:   "refresh",
		Short: "Refresh VLESS links in state.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.RefreshLinks(*paths)
		},
	})
	return links
}

func usersCmd(paths *config.Paths) *cobra.Command {
	users := &cobra.Command{
		Use:   "users",
		Short: "Manage VPN subscribers (no Telegram required)",
	}

	var addLabel string
	var addNever bool
	var addDays int
	add := &cobra.Command{
		Use:   "add USER_ID",
		Short: "Create user, provision on all servers, print links",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.UsersAdd(*paths, args[0], addLabel, addNever, addDays)
		},
	}
	add.Flags().StringVar(&addLabel, "label", "", "subscription label (default user-<id>-happ)")
	add.Flags().BoolVar(&addNever, "never", false, "never-expire subscription")
	add.Flags().IntVar(&addDays, "days", 0, "subscription length in days (default from config)")

	var showFormat string
	show := &cobra.Command{
		Use:   "show USER_ID",
		Short: "Show subscription links for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.UsersShow(*paths, args[0], showFormat)
		},
	}
	show.Flags().StringVar(&showFormat, "format", "links", "links|all|happ|json")

	var exportOut string
	export := &cobra.Command{
		Use:   "export USER_ID",
		Short: "Export subscription message to stdout or file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.UsersExport(*paths, args[0], exportOut)
		},
	}
	export.Flags().StringVarP(&exportOut, "output", "o", "", "output file (default stdout)")

	renew := &cobra.Command{
		Use:   "renew USER_ID DAYS",
		Short: "Extend subscription",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			days, err := parsePositiveInt(args[1])
			if err != nil {
				return err
			}
			return deploy.UsersRenew(*paths, args[0], days)
		},
	}

	never := &cobra.Command{
		Use:   "never USER_ID on|off",
		Short: "Toggle never-expire subscription",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			on, err := parseOnOff(args[1])
			if err != nil {
				return err
			}
			return deploy.UsersNever(*paths, args[0], on)
		},
	}

	users.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List users from state.yaml",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersList(*paths)
			},
		},
		add,
		show,
		export,
		&cobra.Command{
			Use:   "revoke USER_ID",
			Short: "Remove user from state and Xray",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersRevoke(*paths, args[0])
			},
		},
		&cobra.Command{
			Use:   "sync USER_ID",
			Short: "Add user to newly added servers",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersSync(*paths, args[0])
			},
		},
		renew,
		never,
		&cobra.Command{
			Use:   "sweep",
			Short: "Remove expired users from state and Xray",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersSweep(*paths)
			},
		},
	)
	return users
}

func serversCmd(paths *config.Paths) *cobra.Command {
	servers := &cobra.Command{
		Use:   "servers",
		Short: "Inspect configured exit servers",
	}
	servers.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List servers from config.yaml",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.ServersList(*paths)
			},
		},
		&cobra.Command{
			Use:   "traffic [server-id...]",
			Short: "Show RX/TX per server",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.ServersTraffic(*paths, args)
			},
		},
		&cobra.Command{
			Use:   "summary",
			Short: "Users, servers, pending requests overview",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.ServersSummary(*paths)
			},
		},
	)
	return servers
}

func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("days must be a positive integer")
	}
	return n, nil
}

func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "1", "true", "yes":
		return true, nil
	case "off", "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected on or off, got %q", s)
	}
}
