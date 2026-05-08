package senzu

import "context"

// contextKey is an unexported type to avoid collisions with other packages
// that also use context.WithValue. Never use a plain string or int as a key.
type contextKey struct{}

// contextWithRun returns a new context that carries h.
// Called by SenzuClient.Run() before invoking the user's function.
func contextWithRun(ctx context.Context, h *RunHandle) context.Context {
	return context.WithValue(ctx, contextKey{}, h)
}

// RunFromContext retrieves the active RunHandle from ctx, or nil if none is set.
// This is the Go equivalent of AsyncLocalStorage.getStore() (TS) and
// _active_run_var.get() (Python).
func RunFromContext(ctx context.Context) *RunHandle {
	h, _ := ctx.Value(contextKey{}).(*RunHandle)
	return h
}
