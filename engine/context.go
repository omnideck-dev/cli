package engine

import (
	"context"
	"errors"
)

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

// CancelRequested reports whether the shared process context has already
// been cancelled (e.g. by SIGINT/SIGTERM). Callers that need to distinguish
// "this subprocess failed because we killed it" from a genuine failure must
// use this instead of errors.Is(err, context.Canceled): exec.CommandContext's
// default Cancel behavior just calls Process.Kill(), and a killed process
// exits non-zero, so the error os/exec actually returns is a plain
// *exec.ExitError ("signal: killed"), never one wrapping context.Canceled.
func CancelRequested() bool {
	return processCtx.Err() != nil
}

// WrapIfCancelled returns err annotated so errors.Is(err, context.Canceled)
// succeeds if the shared process context was already cancelled when err was
// produced; otherwise it returns err unchanged. A workflow that runs a
// sequence of engine subprocess calls should call this once, at its single
// point of failure, instead of checking errors.Is(err, context.Canceled)
// directly against a subprocess's own error — see CancelRequested's doc for
// why that check does not otherwise work. Must be called before any
// SetCancelContext reset (e.g. for best-effort cleanup), or the
// cancellation will no longer be observable.
func WrapIfCancelled(err error) error {
	if err == nil || !CancelRequested() {
		return err
	}
	return errors.Join(context.Canceled, err)
}
