package deploy

import "fmt"

func phase(title string) {
	fmt.Printf("\n>>> %s\n", title)
}

func step(n, total int, msg string) {
	fmt.Printf("  [%d/%d] %s\n", n, total, msg)
}

func logf(msg string, args ...interface{}) {
	fmt.Printf("  · "+msg+"\n", args...)
}

func logOK(msg string, args ...interface{}) {
	fmt.Printf("  ✓ "+msg+"\n", args...)
}

func connectMsg(host string) {
	logf("SSH connect %s ...", host)
}

func connectedMsg(host string) {
	logOK("SSH connected: %s", host)
}
