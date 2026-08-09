//go:build !windows

package engine

import "os/exec"

// prepareHiddenConsoleCommand is intentionally a no-op off Windows. The
// process intent stays explicit at call sites even though Unix hosts do not
// have a console-window creation flag.
func prepareHiddenConsoleCommand(_ *exec.Cmd) {}

func prepareVisibleCommand(_ *exec.Cmd) {}
