package engine

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	ollamaAPICheckPath = "/api/tags"
	podmanHostAlias    = "host.containers.internal"
)

func defaultOllamaURL(runtimeName, platform string) string {
	host := podmanHostAlias + ":11434"
	if platform == "darwin" {
		host = "host.docker.internal:11434"
	}
	_ = runtimeName
	return "http://" + host
}

func usesPodmanHostAlias(value, platform string) bool {
	url := normalizeOllamaURL(value, "podman", platform)
	host := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")
	host = strings.SplitN(host, "/", 2)[0]
	return strings.EqualFold(strings.SplitN(host, ":", 2)[0], podmanHostAlias)
}

func normalizeOllamaURL(value, runtimeName, platform string) string {
	if value == "" {
		return defaultOllamaURL(runtimeName, platform)
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "http://" + value
}

func checkContainerOllama(binary, name string) error {
	url := defaultOllamaURL(binary, runtime.GOOS) + ollamaAPICheckPath
	commands := [][]string{
		{"exec", name, "curl", "--fail", "--silent", "--show-error", "--max-time", "4", "--output", "/dev/null", url},
		{"exec", name, "wget", "--quiet", "--timeout=4", "--output-document=/dev/null", url},
		{"exec", name, "python3", "-c", "import sys, urllib.request; urllib.request.urlopen(sys.argv[1], timeout=4).read(1)", url},
	}

	var missingTools []string
	for _, args := range commands {
		cmd := buildCmd(binary, args...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if commandMissing(err, string(out)) {
			missingTools = append(missingTools, args[2])
			continue
		}
		return fmt.Errorf("%s could not reach %s: %w: %s", name, url, err, strings.TrimSpace(string(out)))
	}
	return fmt.Errorf("%s has no supported connection-check tool (%s)", name, strings.Join(missingTools, ", "))
}

func commandMissing(err error, output string) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 127 {
		return true
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "executable file not found") ||
		strings.Contains(lower, "executable not found") ||
		strings.Contains(lower, "no such file or directory")
}
