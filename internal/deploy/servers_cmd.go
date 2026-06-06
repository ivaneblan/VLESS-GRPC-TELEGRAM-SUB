package deploy

import (
	"fmt"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

func ServersList(paths config.Paths) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	fmt.Println(m.FormatServersList())
	return nil
}

func ServersTraffic(paths config.Paths, serverIDs []string) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	targets := serverIDs
	if len(targets) == 0 {
		for _, s := range m.Cfg.Servers {
			targets = append(targets, s.ID)
		}
	}
	for i, id := range targets {
		server := m.Cfg.ServerByID(id)
		if server == nil {
			return fmt.Errorf("unknown server id: %s", id)
		}
		step(i+1, len(targets), fmt.Sprintf("traffic %s (%s)", server.Name, server.Host))
		stats, err := m.GetServerTrafficStats(server)
		if err != nil {
			return err
		}
		logOK("%s: iface=%s RX=%s GB TX=%s GB", server.Name, stats.Iface, stats.RXGB, stats.TXGB)
	}
	return nil
}

func ServersSummary(paths config.Paths) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	fmt.Println(m.FormatSummary(st))
	return nil
}
