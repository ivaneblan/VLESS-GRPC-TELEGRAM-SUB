package deploy

import (
	"os"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/logx"
)

func copyIfMissing(dst, src string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := copyFile(src, dst); err != nil {
		return
	}
	logx.Infof("created %s", dst)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
