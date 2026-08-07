package engine

// podmanPlatformPolicy is the single source of truth for host-level Podman
// differences. Higher layers consume probes and setup plans instead of
// rediscovering whether the current OS has a machine/connection layer.
type podmanPlatformPolicy struct {
	UsesMachine bool
	MachineName string
}

func podmanPolicy(goos string) podmanPlatformPolicy {
	switch goos {
	case "darwin", "windows":
		return podmanPlatformPolicy{
			UsesMachine: true,
			MachineName: OmnideckMachineName,
		}
	default:
		return podmanPlatformPolicy{}
	}
}

// podmanCommandArgs keeps every ordinary Podman operation on the machine that
// Omnideck owns on macOS and Windows. Machine-management commands are left
// unscoped because they create/start the connection itself. Linux has no
// Podman machine layer and therefore receives no connection flag.
func podmanCommandArgs(goos string, args ...string) []string {
	result := append([]string(nil), args...)
	policy := podmanPolicy(goos)
	if !policy.UsesMachine {
		return result
	}
	if len(args) > 0 && args[0] == "machine" {
		return result
	}
	return append([]string{"--connection", policy.MachineName}, result...)
}
