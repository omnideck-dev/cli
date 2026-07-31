package engine

import "context"

// processCtx is the context used to build every underlying docker/podman
// subprocess. It defaults to a context that is never cancelled so normal
// library use (tests, callers that never call SetCancelContext) is
// unaffected.
var processCtx = context.Background()

// SetCancelContext installs the context every subsequent engine-invoked
// subprocess is built with. The CLI entrypoint calls this once with a
// context tied to SIGINT/SIGTERM so a killed omnideck process also kills
// whatever docker/podman subprocess it was waiting on, instead of orphaning
// it. This is intentionally a package-level seam rather than a new
// exported Engine method or parameter: there is exactly one process-wide
// cancellation scope for a CLI, not a per-call one.
func SetCancelContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	processCtx = ctx
}
