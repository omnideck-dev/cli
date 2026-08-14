//go:build darwin

package engine

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const podmanServiceRequestTimeout = 2500 * time.Millisecond

func newUnixPodmanServiceClient(socketPath string) *podmanServiceClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &podmanServiceClient{
		client:  &http.Client{Transport: transport, Timeout: podmanServiceRequestTimeout},
		baseURL: "http://podman",
	}
}

var darwinPodmanServiceClient = sync.OnceValue(func() *podmanServiceClient {
	socket := filepath.Join(os.TempDir(), "podman", OmnideckMachineName+"-api.sock")
	return newUnixPodmanServiceClient(socket)
})

// hostPodmanServiceClient keeps periodic dashboard reads inside this process.
// Terminal.app tracks short-lived descendants even when they have their own
// session, so launching `podman` every poll still changes the terminal title.
func hostPodmanServiceClient() (*podmanServiceClient, bool) {
	return darwinPodmanServiceClient(), true
}
