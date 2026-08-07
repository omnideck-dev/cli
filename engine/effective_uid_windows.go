//go:build windows

package engine

// Windows never uses the Linux package-manager installation path. Keeping the
// implementation here lets the shared setup package cross-compile cleanly.
func effectiveUserID() int {
	return -1
}
