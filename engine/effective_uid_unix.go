//go:build linux || darwin

package engine

import "os"

func effectiveUserID() int {
	return os.Geteuid()
}
