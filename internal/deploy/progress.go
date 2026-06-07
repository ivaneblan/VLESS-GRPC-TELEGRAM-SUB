package deploy

import "github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"

func phase(title string) {
	logx.Phase(title)
}

func step(n, total int, msg string) {
	logx.Step(n, total, msg)
}

func logf(msg string, args ...interface{}) {
	logx.Infof(msg, args...)
}

func logOK(msg string, args ...interface{}) {
	logx.Infof(msg, args...)
}

func logErr(msg string, args ...interface{}) {
	logx.Errf(msg, args...)
}

func connectMsg(host string) {
	logf("SSH connect %s ...", host)
}

func connectedMsg(host string) {
	logOK("SSH connected: %s", host)
}
