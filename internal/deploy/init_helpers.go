package deploy

import (
	"fmt"
	"os"
)

func copyIfMissing(dst, src string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := copyFile(src, dst); err != nil {
		return
	}
	fmt.Println("created", dst)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
