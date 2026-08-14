//go:build !darwin

package engine

func hostPodmanServiceClient() (*podmanServiceClient, bool) {
	return nil, false
}
