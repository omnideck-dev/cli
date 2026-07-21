package checks

import (
	"net"
	"regexp"
	"strconv"
)

var containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var memorySizePattern = regexp.MustCompile(`^\d+[mMgGkK]$`)

// ValidContainerName applies the common Docker and Podman container-name
// rules used by Omnideck setup.
func ValidContainerName(name string) bool {
	return containerNamePattern.MatchString(name)
}

// ValidMemorySize accepts the memory syntax supported by Docker and Podman in
// Omnideck configuration, such as 512m or 2g.
func ValidMemorySize(value string) bool {
	return memorySizePattern.MatchString(value)
}

// ValidPort reports whether value is a valid TCP port number.
func ValidPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

// PortAvailable checks whether a local browser address can be reserved. The
// listener is closed immediately; the container runtime performs the final,
// authoritative check when it starts the container.
func PortAvailable(value string) bool {
	if !ValidPort(value) {
		return false
	}
	listener, err := net.Listen("tcp", ":"+value)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// NextAvailablePort returns the first valid, unused port at or above start
// that is not reserved by an existing Omnideck configuration.
func NextAvailablePort(start int, reserved map[string]bool) (string, bool) {
	if start < 1 {
		start = 1
	}
	for port := start; port <= 65535; port++ {
		value := strconv.Itoa(port)
		if !reserved[value] && PortAvailable(value) {
			return value, true
		}
	}
	return "", false
}
