package checks

import (
	"net"
	"strconv"
	"testing"
)

func TestValidContainerName(t *testing.T) {
	for _, name := range []string{"omnideck", "omnideck-2", "box_1", "box.one", "X123"} {
		if !ValidContainerName(name) {
			t.Errorf("ValidContainerName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "-leading", ".leading", "has space", "has/slash"} {
		if ValidContainerName(name) {
			t.Errorf("ValidContainerName(%q) = true", name)
		}
	}
}

func TestValidPort(t *testing.T) {
	for _, port := range []string{"1", "2337", "65535"} {
		if !ValidPort(port) {
			t.Errorf("ValidPort(%q) = false", port)
		}
	}
	for _, port := range []string{"", "0", "65536", "abc", "12.5"} {
		if ValidPort(port) {
			t.Errorf("ValidPort(%q) = true", port)
		}
	}
}

func TestValidMemorySize(t *testing.T) {
	for _, value := range []string{"512m", "2g", "128K"} {
		if !ValidMemorySize(value) {
			t.Errorf("ValidMemorySize(%q) = false", value)
		}
	}
	for _, value := range []string{"", "0g", "512", "2gb", "1.5g"} {
		if ValidMemorySize(value) {
			t.Errorf("ValidMemorySize(%q) = true", value)
		}
	}
}

func TestPortAvailableAndNextAvailablePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupied := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if PortAvailable(occupied) {
		t.Fatalf("PortAvailable(%s) = true while listener is open", occupied)
	}

	start, _ := strconv.Atoi(occupied)
	if start == 65535 {
		t.Skip("ephemeral listener received the last TCP port")
	}
	next, ok := NextAvailablePort(start, map[string]bool{})
	if !ok || next == occupied {
		t.Fatalf("NextAvailablePort(%d) = %q, %v", start, next, ok)
	}
}
