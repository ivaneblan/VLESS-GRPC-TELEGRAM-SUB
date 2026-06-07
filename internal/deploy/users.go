package deploy

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/subscription"
)

func manager(paths config.Paths) (*subscription.Manager, error) {
	cfg, sec, _, err := config.LoadAll(paths)
	if err != nil {
		return nil, err
	}
	return subscription.NewManager(cfg, sec, paths), nil
}

func UsersList(paths config.Paths) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	fmt.Println(m.FormatUsersList(st))
	return nil
}

func UsersAdd(paths config.Paths, userID, label string, never bool, days int) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	logf("provision user %s on all servers", userID)
	linksMap, err := m.AddUser(userID, label, never, days)
	if err != nil {
		return err
	}
	st, _ := m.LoadState()
	u := st.Users[userID]
	logOK("user %s provisioned (uuid %s)", userID, u.UUID)
	fmt.Println(m.BuildSubscriptionMessage(linksMap, m.ExpiresAtInt(&u)))
	return nil
}

func UsersShow(paths config.Paths, userID, format string) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	userData, ok := st.Users[userID]
	if !ok {
		return fmt.Errorf("user %s not found", userID)
	}
	if len(userData.Servers) == 0 {
		return fmt.Errorf("user %s has no links — run: vpnctl users add %s", userID, userID)
	}
	switch format {
	case "json":
		out, err := subscription.FormatLinksJSON(userData.Servers)
		if err != nil {
			return err
		}
		fmt.Println(out)
	case "all":
		fmt.Println(subscription.FormatLinksPlain(userData.Servers, false))
	case "happ":
		fmt.Println(m.BuildSubscriptionMessage(userData.Servers, m.ExpiresAtInt(&userData)))
	default:
		fmt.Println(subscription.FormatLinksPlain(userData.Servers, true))
	}
	return nil
}

func UsersRevoke(paths config.Paths, userID string) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	removed, err := m.RevokeUser(userID)
	if err != nil {
		return err
	}
	if len(removed) > 0 {
		logOK("user %s removed from state and xray on: %s", userID, strings.Join(removed, ", "))
	} else {
		logOK("user %s removed from state", userID)
	}
	return nil
}

func UsersSync(paths config.Paths, userID string) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	added, err := m.SyncUser(userID)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		logOK("user %s already on all servers", userID)
		return nil
	}
	logOK("user %s synced, added: %s", userID, strings.Join(added, ", "))
	return nil
}

func UsersSyncAll(paths config.Paths) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	if len(st.Users) == 0 {
		logOK("no users to sync")
		return nil
	}
	ids := make([]string, 0, len(st.Users))
	for id := range st.Users {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var changed, failed int
	for _, id := range ids {
		added, err := m.SyncUser(id)
		if err != nil {
			logErr("user %s: %v", id, err)
			failed++
			continue
		}
		if len(added) > 0 {
			logOK("user %s synced, added: %s", id, strings.Join(added, ", "))
			changed++
		}
	}
	logOK("sync-all done: %d updated, %d already in sync, %d failed", changed, len(ids)-changed-failed, failed)
	if failed > 0 {
		return fmt.Errorf("%d user(s) failed to sync", failed)
	}
	return nil
}

func UsersRenew(paths config.Paths, userID string, days int) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	exp, err := m.RenewUser(userID, days)
	if err != nil {
		return err
	}
	logOK("user %s renewed +%d days, expires %s", userID, days, m.FormatTS(exp))
	return nil
}

func UsersNever(paths config.Paths, userID string, on bool) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	if err := m.SetNeverExpires(userID, on); err != nil {
		return err
	}
	state := "disabled"
	if on {
		state = "enabled"
	}
	logOK("never-expires %s for user %s", state, userID)
	return nil
}

func UsersSweep(paths config.Paths) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	res := m.SweepExpiredUsers(st, 0)
	if len(res.RemovedUsers) > 0 {
		if err := m.SaveState(st); err != nil {
			return err
		}
		logOK("removed expired users: %s", strings.Join(res.RemovedUsers, ", "))
	} else if res.Skipped {
		fmt.Println("sweep skipped (recently run)")
	} else {
		fmt.Println("no expired users")
	}
	return nil
}

func UsersExport(paths config.Paths, userID, outPath string) error {
	m, err := manager(paths)
	if err != nil {
		return err
	}
	st, err := m.LoadState()
	if err != nil {
		return err
	}
	userData, ok := st.Users[userID]
	if !ok {
		return fmt.Errorf("user %s not found", userID)
	}
	text := m.BuildSubscriptionMessage(userData.Servers, m.ExpiresAtInt(&userData))
	if outPath == "" || outPath == "-" {
		fmt.Println(text)
		return nil
	}
	return os.WriteFile(outPath, []byte(text), 0o600)
}
