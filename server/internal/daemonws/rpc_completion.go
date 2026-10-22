package daemonws

import "context"

type rpcCompletionKey struct{}

type rpcCompletion struct {
	callbacks []func()
}

// AfterRPCResponse registers synchronous request cleanup to run after the RPC
// response is queued. The existing handler goroutine and concurrency slot stay
// owned until cleanup finishes; no detached worker is started. Call only from
// the RPC handler goroutine. False means this is not a hub-dispatched request.
func AfterRPCResponse(ctx context.Context, cleanup func()) bool {
	completion, _ := ctx.Value(rpcCompletionKey{}).(*rpcCompletion)
	if completion == nil {
		return false
	}
	completion.callbacks = append(completion.callbacks, cleanup)
	return true
}

func (c *rpcCompletion) run() {
	for _, cleanup := range c.callbacks {
		cleanup()
	}
}
