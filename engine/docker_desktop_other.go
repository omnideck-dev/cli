//go:build !windows

package engine

// WSL uses Windows PowerShell to find Docker Desktop on the Windows side. This
// implementation is not included in the Windows executable.
func startDockerDesktopCommand() SetupCommand {
	return SetupCommand{
		Name: "powershell.exe",
		Args: []string{"-NoProfile", "-Command", `$paths = @("$Env:LOCALAPPDATA\Programs\DockerDesktop\Docker Desktop.exe", "$Env:ProgramFiles\Docker\Docker\Docker Desktop.exe"); ` +
			`$app = $paths | Where-Object { Test-Path $_ } | Select-Object -First 1; ` +
			`if (-not $app) { throw "Docker Desktop was not found" }; Start-Process $app`},
		Display: "Start Docker Desktop",
	}
}
