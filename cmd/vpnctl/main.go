package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/deploy"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
	"github.com/spf13/cobra"
)

// Populated at build time via -ldflags (see build.ps1).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Command groups shown in `vpnctl --help`.
const (
	groupSetup  = "setup"
	groupDeploy = "deploy"
	groupManage = "manage"
	groupOps    = "ops"
)

func main() {
	rootDir, _ := os.Getwd()
	paths := config.DefaultPaths(rootDir)

	root := &cobra.Command{
		Use:   "vpnctl",
		Short: "VLESS gRPC VPN deploy CLI",
		Long: `vpnctl deploys and manages a VLESS+gRPC Reality VPN fleet:
provisions exit servers, manages subscribers, and ships an optional Telegram bot.

Typical first run:
  vpnctl init                 # create config/secrets templates + SSH keys
  # edit config.yaml & secrets.yaml
  vpnctl bootstrap            # fresh install on all servers
  vpnctl users add alice      # create a subscriber and print links`,
		Version:           buildVersion(),
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: false},
	}
	root.SetVersionTemplate("vpnctl {{.Version}}\n")

	var (
		logJSON bool
		verbose bool
	)
	root.PersistentFlags().StringVarP(&paths.Root, "root", "C", paths.Root, "project root directory")
	root.PersistentFlags().BoolVar(&logJSON, "log-json", false, "emit logs as JSON instead of pretty console")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose (debug-level) logging")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		logx.Setup(logJSON, verbose)
		// Honour ssh.strict_host_key from config when it is available (it is not
		// during `vpnctl init`, before config.yaml exists).
		p := config.DefaultPaths(paths.Root)
		if cfg, err := config.LoadConfig(p.ConfigPath); err == nil {
			sshclient.StrictHostKey = cfg.SSH.StrictHostKey
		}
	}

	root.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "Setup & Lifecycle:"},
		&cobra.Group{ID: groupDeploy, Title: "Deploy & Components:"},
		&cobra.Group{ID: groupManage, Title: "Subscribers & Servers:"},
		&cobra.Group{ID: groupOps, Title: "Maintenance & Monitoring:"},
	)

	root.AddCommand(
		// Setup & lifecycle
		simpleCmd(&paths, groupSetup, "init", "Create YAML templates and SSH keys", deploy.InitProject),
		bootstrapCmd(&paths),
		allCmd(&paths),
		redeployCmd(&paths),

		// Deploy & components
		simpleCmd(&paths, groupDeploy, "keys", "Install SSH public key on all servers", deploy.Keys),
		vlessCmd(&paths),
		botCmd(&paths),
		linksCmd(&paths),

		// Subscribers & servers
		usersCmd(&paths),
		serversCmd(&paths),

		// Maintenance & monitoring
		checkCmd(&paths),
		simpleCmd(&paths, groupOps, "backup", "Backup state.yaml, config.yaml, secrets.yaml to backups/", deploy.Backup),
		simpleCmd(&paths, groupOps, "cleanup", "Remove xray, bot, legacy services from all servers", deploy.Cleanup),
		passwdCmd(&paths),
	)

	if err := root.Execute(); err != nil {
		logx.Errf("%v", err)
		os.Exit(1)
	}
}

func buildVersion() string {
	return fmt.Sprintf("%s (commit %s, built %s, %s)", version, commit, date, runtime.Version())
}

// simpleCmd builds a no-arg command that just reloads paths and runs fn.
func simpleCmd(paths *config.Paths, group, use, short string, fn func(config.Paths) error) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		GroupID: group,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return fn(*paths)
		},
	}
}

func checkCmd(paths *config.Paths) *cobra.Command {
	return &cobra.Command{
		Use:     "check",
		Aliases: []string{"health"},
		Short:   "Health check all exit servers",
		GroupID: groupOps,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Health(*paths)
		},
	}
}

func allCmd(paths *config.Paths) *cobra.Command {
	var skipBot bool
	c := &cobra.Command{
		Use:     "all",
		Short:   "Full deploy: keys + vless + links + bot + check",
		GroupID: groupSetup,
		Example: "  vpnctl all\n  vpnctl all --no-bot",
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
		Use:     "redeploy",
		Short:   "Reinstall with existing subscribers: backup + cleanup + full deploy",
		GroupID: groupSetup,
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
		Use:     "passwd [server-id...]",
		Short:   "Change root password and update secrets.yaml",
		GroupID: groupOps,
		Long: `Safely rotates root password on VPS nodes.

  1. Backs up secrets.yaml
  2. Ensures SSH public key is in authorized_keys (recovery path)
  3. Sets new password on the server
  4. Verifies password login before saving
  5. Reverts on failure; updates secrets.yaml only after success`,
		Example: "  vpnctl passwd --generate          # all servers, random password\n" +
			"  vpnctl passwd de --password '...' # one server, explicit password",
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
		Use:     "bootstrap",
		Short:   "Fresh install on new servers (empty state, no backup)",
		GroupID: groupSetup,
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
		Use:     "vless [server-id...]",
		Short:   "Deploy VLESS+gRPC Reality on servers",
		GroupID: groupDeploy,
		Example: "  vpnctl vless              # all servers\n" +
			"  vpnctl vless de nl        # specific servers\n" +
			"  vpnctl vless --new-keys   # rotate Reality keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Vless(*paths, args, newKeys, false)
		},
	}
	c.Flags().BoolVar(&newKeys, "new-keys", false, "regenerate Reality keys")
	return c
}

func botCmd(paths *config.Paths) *cobra.Command {
	var forceState bool
	bot := &cobra.Command{
		Use:     "bot",
		Short:   "Deploy Telegram bot (tgbot) via systemd",
		GroupID: groupDeploy,
		Long: `Deploy the Telegram bot, or sync its state.

Run without a subcommand to (re)deploy the bot. The bot keeps its own
state.yaml on the bot server and writes users added via Telegram there.
By default the deploy keeps a non-empty remote state.yaml; run
"vpnctl bot pull-state" first to copy those users back locally, or pass
--force-state to overwrite the server state with your local copy.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.Bot(*paths, forceState)
		},
	}
	bot.Flags().BoolVar(&forceState, "force-state", false, "overwrite remote state.yaml with the local copy")
	bot.AddCommand(&cobra.Command{
		Use:   "pull-state",
		Short: "Download state.yaml from the bot server (backs up local first)",
		Long: `Copies the live state.yaml from the bot server into your local state.yaml,
backing up the current local file as state.yaml.<timestamp>.bak.

Use this to pick up users that were added through the Telegram bot before
running any CLI command or redeploying the bot.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			return deploy.BotPullState(*paths)
		},
	})
	return bot
}

func linksCmd(paths *config.Paths) *cobra.Command {
	links := &cobra.Command{
		Use:     "links",
		Short:   "Manage subscription links",
		GroupID: groupDeploy,
	}
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
		Use:     "users",
		Aliases: []string{"user", "u"},
		Short:   "Manage VPN subscribers (no Telegram required)",
		GroupID: groupManage,
	}

	var addLabel string
	var addNever bool
	var addDays int
	add := &cobra.Command{
		Use:     "add USER_ID",
		Short:   "Create user, provision on all servers, print links",
		Args:    cobra.ExactArgs(1),
		Example: "  vpnctl users add alice\n  vpnctl users add bob --days 30\n  vpnctl users add vip --never",
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

	var syncAll bool
	sync := &cobra.Command{
		Use:     "sync [USER_ID]",
		Short:   "Add user to newly added servers (use --all for everyone)",
		Example: "  vpnctl users sync alice\n  vpnctl users sync --all",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			*paths = config.DefaultPaths(paths.Root)
			if syncAll {
				if len(args) > 0 {
					return fmt.Errorf("--all syncs every user; do not pass USER_ID")
				}
				return deploy.UsersSyncAll(*paths)
			}
			if len(args) != 1 {
				return fmt.Errorf("provide USER_ID or use --all")
			}
			return deploy.UsersSync(*paths, args[0])
		},
	}
	sync.Flags().BoolVar(&syncAll, "all", false, "sync all users to all servers")

	users.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List users from state.yaml",
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersList(*paths)
			},
		},
		add,
		show,
		export,
		&cobra.Command{
			Use:     "revoke USER_ID",
			Aliases: []string{"rm", "delete"},
			Short:   "Remove user from state and Xray",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				*paths = config.DefaultPaths(paths.Root)
				return deploy.UsersRevoke(*paths, args[0])
			},
		},
		sync,
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
		Use:     "servers",
		Aliases: []string{"server", "srv"},
		Short:   "Inspect configured exit servers",
		GroupID: groupManage,
	}
	servers.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List servers from config.yaml",
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
