package checks

import (
	"net"
	"time"
)

// OllamaStatus reports only whether Ollama is running on this computer. It does
// not claim that a container can reach it; that path is checked separately.
type OllamaStatus struct {
	Running bool
	Host    string
}

// OllamaHost returns the address at which Ollama is reachable from the host.
// The CLI runs on the host and Ollama listens on the host loopback, so this is
// 127.0.0.1 on every platform.
//
// This is deliberately NOT the address a container uses to reach Ollama (e.g.
// host.docker.internal). Earlier this returned host.docker.internal on macOS,
// which is a Docker-internal DNS name that only resolves inside containers — so
// the host-side dial below always failed on macOS even when Ollama was running.
// The container-facing address is computed separately in the engine package.
func OllamaHost() string {
	return "127.0.0.1:11434"
}

// CheckOllama attempts a TCP dial to the Ollama port on the host.
// Returns (reachable, host).
func CheckOllama() (bool, string) {
	status := CheckOllamaStatus()
	return status.Running, status.Host
}

// CheckOllamaStatus checks only whether Ollama is running on this computer.
// Container access is tested separately from inside the running Omnideck
// container; a host loopback check cannot prove that network path.
func CheckOllamaStatus() OllamaStatus {
	host := OllamaHost()
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return OllamaStatus{Host: host}
	}
	_ = conn.Close()
	return OllamaStatus{Running: true, Host: host}
}
