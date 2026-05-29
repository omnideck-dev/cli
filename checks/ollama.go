package checks

import (
	"net"
	"runtime"
	"time"
)

// OllamaHost returns the OS-aware Ollama host:port string.
func OllamaHost() string {
	if runtime.GOOS == "darwin" {
		return "host.docker.internal:11434"
	}
	return "127.0.0.1:11434"
}

// CheckOllama attempts a TCP dial to the Ollama port.
// Returns (reachable, host).
func CheckOllama() (bool, string) {
	host := OllamaHost()
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false, host
	}
	conn.Close()
	return true, host
}
