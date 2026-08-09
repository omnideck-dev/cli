package engine

import (
	"runtime"
	"strings"
)

// hostCommandEnvironment prevents an AppImage's private dynamic-loader paths
// from leaking into host tools such as conmon. The desktop host and bundled
// CLI share the AppImage process environment, but conmon must load the host
// distribution's GLib rather than the AppImage's bundled copy.
func hostCommandEnvironment(environment []string) []string {
	if runtime.GOOS != "linux" {
		return environment
	}

	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "LD_LIBRARY_PATH", "LD_PRELOAD", "LD_AUDIT":
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
