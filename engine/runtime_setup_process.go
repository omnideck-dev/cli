package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

func runQuietSetupCommand(name string, args, environment []string, accepted []int, onWait func()) error {
	return runSetupCommand(name, args, environment, accepted, onWait, true)
}

// runVisibleSetupCommand is for GUI installers whose own UI or security
// prompt must remain available to the user. The caller should still pass
// noninteractive installer arguments when the installer window itself is not
// useful; visibility here means we never accidentally hide a required prompt.
func runVisibleSetupCommand(name string, args, environment []string, accepted []int, onWait func()) error {
	return runSetupCommand(name, args, environment, accepted, onWait, false)
}

func runSetupCommand(name string, args, environment []string, accepted []int, onWait func(), hideConsole bool) error {
	return runObservedSetupCommand(name, args, environment, accepted, nil, onWait, hideConsole)
}

// runObservedQuietSetupCommand adds a lightweight poll hook for installers
// whose native authorization prompt and background work happen inside one
// long-running process. The hook lets the caller advance the customer-facing
// state only after it can prove installation has begun.
func runObservedQuietSetupCommand(name string, args, environment []string, accepted []int, onObserve func()) error {
	return runObservedSetupCommand(name, args, environment, accepted, onObserve, nil, true)
}

func runObservedSetupCommand(name string, args, environment []string, accepted []int, onObserve, onWait func(), hideConsole bool) error {
	commandContext, cancel := context.WithTimeout(processCtx, 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandContext, name, args...)
	if hideConsole {
		prepareHiddenConsoleCommand(command)
	} else {
		prepareVisibleCommand(command)
	}
	command.Env = hostCommandEnvironment(environment)
	var stdoutTail, stderrTail tailBuffer
	command.Stdout = &stdoutTail
	command.Stderr = &stderrTail
	done := make(chan error, 1)
	go func() {
		err := command.Run()
		if err != nil {
			err = runtimeCommandError(name, err, joinCommandOutput(stdoutTail.Bytes(), stderrTail.Bytes()))
		}
		done <- err
	}()
	waitTimer := time.NewTimer(90 * time.Second)
	defer waitTimer.Stop()
	waitChannel := waitTimer.C
	var observeChannel <-chan time.Time
	var observeTicker *time.Ticker
	if onObserve != nil {
		observeTicker = time.NewTicker(250 * time.Millisecond)
		observeChannel = observeTicker.C
		defer observeTicker.Stop()
	}
	for {
		select {
		case err := <-done:
			if err == nil || containsInt(accepted, exitCode(err)) {
				return nil
			}
			return err
		case <-observeChannel:
			onObserve()
		case <-waitChannel:
			waitChannel = nil
			if onWait != nil {
				onWait()
			}
		case <-commandContext.Done():
			return context.Cause(commandContext)
		}
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// retainBoundedLog keeps the most useful tail of an installer log on disk and
// returns the same bytes for inline technical details. Installer logs live in
// the user's cache, but a stuck or unusually verbose installer must not grow
// that cache without a bound.
func retainBoundedLog(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() > maxCommandOutput {
		if _, err := file.Seek(info.Size()-maxCommandOutput, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maxCommandOutput))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if info.Size() > maxCommandOutput {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			return nil, err
		}
	}
	return contents, nil
}
