// Package debug exposes the --debug flag state to other packages
// without creating an import cycle with the cmd package.
package debug

var enabled bool

// SetEnabled sets the debug flag state. Called from cmd/root.go.
func SetEnabled(v bool) { enabled = v }

// Enabled returns true if --debug was passed.
func Enabled() bool { return enabled }
