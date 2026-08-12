// Package debuglog owns the process-wide diagnostic switch used below the
// command layer. Keeping it dependency-free prevents runtime code from
// importing CLI presentation packages.
package debuglog

var enabled bool

func SetEnabled(value bool) { enabled = value }
func Enabled() bool         { return enabled }
