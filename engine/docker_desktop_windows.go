//go:build windows

package engine

// startDockerDesktopCommand launches Docker Desktop directly on Windows. The
// probe has already checked the standard per-user and all-user locations, so a
// stopped installation normally resolves to an absolute executable path.
func startDockerDesktopCommand() SetupCommand {
	name := installedDockerDesktopPath()
	if name == "" {
		name = "Docker Desktop.exe"
	}
	return SetupCommand{
		Name:    name,
		Display: "Start Docker Desktop",
	}
}
