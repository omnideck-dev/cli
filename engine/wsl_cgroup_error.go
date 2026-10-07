package engine

import "strings"

// WSLCgroupError identifies the WSL/Podman controller failure observed during
// container startup. Recovery changes Windows-wide WSL settings and must remain
// an explicit user action; the CLI must not remove container resource limits.
type WSLCgroupError struct{ Err error }

const WSLCgroupTitle = "Windows' Linux runtime could not apply container limits"
const WSLCgroupRecovery = "Review the Windows WSL recovery steps at https://www.omnideck.dev/install.html#windows-wsl-recovery, then restart the runtime and try again. WSL settings affect every Linux distribution on this Windows account. Your memory limit remains enabled; omnideck has not changed those settings."

func (e *WSLCgroupError) Error() string {
	return WSLCgroupTitle + ". " + WSLCgroupRecovery + "\n" + e.Err.Error()
}
func (e *WSLCgroupError) Unwrap() error { return e.Err }

func isWindowsWSLCgroupFailure(goos, action, detail string) bool {
	if goos != "windows" || (action != "podman run" && action != "podman start") {
		return false
	}
	lower := strings.ToLower(detail)
	if !strings.Contains(lower, "crun:") && !strings.Contains(lower, "runc:") {
		return false
	}
	return (strings.Contains(lower, "memory.max") && strings.Contains(lower, "no such file or directory")) ||
		(strings.Contains(lower, "controller") && strings.Contains(lower, "is not available") && strings.Contains(lower, "/wsl-user/"))
}
