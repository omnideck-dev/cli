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
	_, ok := MemorySizeMB(value)
	return ok
}

// MemorySizeMB parses the memory syntax accepted by Omnideck into MiB.
func MemorySizeMB(value string) (int64, bool) {
	if !memorySizePattern.MatchString(value) {
		return 0, false
	}
	amount, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	if err != nil || amount <= 0 {
		return 0, false
	}
	switch value[len(value)-1] {
	case 'g', 'G':
		return amount * 1024, true
	case 'm', 'M':
		return amount, true
	case 'k', 'K':
		return (amount + 1023) / 1024, true
	default:
		return 0, false
	}
}

// ValidPort reports whether value is a valid TCP port number.
func ValidPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

// PortAvailable checks whether the IPv4 browser address used by Omnideck can
// be reserved. The listener is closed immediately; the container runtime
// performs the final, authoritative check when it starts the container.
func PortAvailable(value string) bool {
	if !ValidPort(value) {
		return false
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+value)
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
