package engine

import "testing"

func TestPodmanPlatformPolicyContainsEveryOSDifference(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		policy := podmanPolicy(goos)
		if !policy.UsesMachine || policy.MachineName != OmnideckMachineName {
			t.Fatalf("%s policy = %#v", goos, policy)
		}
	}
	for _, goos := range []string{"linux", "freebsd"} {
		policy := podmanPolicy(goos)
		if policy.UsesMachine || policy.MachineName != "" {
			t.Fatalf("%s policy = %#v, want native Podman", goos, policy)
		}
	}
}
