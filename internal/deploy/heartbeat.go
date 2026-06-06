package deploy

import (
	"time"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/sshclient"
)

func runScriptWithHeartbeat(client *sshclient.Client, script string, timeout time.Duration, label string) (int, string, string) {
	type result struct {
		rc         int
		out, errStr string
	}
	done := make(chan result, 1)
	go func() {
		rc, out, errStr := client.RunScriptLive(script, timeout)
		done <- result{rc, out, errStr}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	elapsed := 0
	for {
		select {
		case r := <-done:
			return r.rc, r.out, r.errStr
		case <-ticker.C:
			elapsed += 20
			logf("%s still running (%ds elapsed)...", label, elapsed)
		}
	}
}
