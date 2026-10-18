package runtime

import "context"

type immediateApplyKey struct{}

// WithImmediateApply requires the remote update to confirm application to a running core.
func WithImmediateApply(ctx context.Context) context.Context {
	return context.WithValue(ctx, immediateApplyKey{}, true)
}

func requiresImmediateApply(ctx context.Context) bool {
	value, _ := ctx.Value(immediateApplyKey{}).(bool)
	return value
}
