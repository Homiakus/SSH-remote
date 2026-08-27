//go:build !windows

package system

import "os"

func currentEUID() int {
	return os.Geteuid()
}
