//go:build !windows && !darwin

package engine

import "os/exec"

// prepareHiddenConsoleCommand is intentionally a no-op on Unix hosts other
// than macOS. The process intent stays explicit at call sites even though
// these hosts do not have a console-window creation flag.
func prepareHiddenConsoleCommand(_ *exec.Cmd) {}

func prepareVisibleCommand(_ *exec.Cmd) {}
