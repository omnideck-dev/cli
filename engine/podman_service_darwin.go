//go:build darwin

package engine

import (
	"os"
	"path/filepath"
	"sync"
)

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
