package analytics

// Event is a single analytics capture.
type Event struct {
	Name        string
	DistinctID  string
	WorkspaceID string
	Properties  map[string]any
	SetOnce     map[string]any
	Set         map[string]any
}

// Client is the narrow surface the rest of the codebase depends on.
type Client interface {
	Capture(e Event)
	Close()
}

// NewFromEnv always returns a no-op client.
func NewFromEnv() Client {
	return NoopClient{}
}

// NoopClient silently drops all events.
type NoopClient struct{}

func (NoopClient) Capture(Event) {}
func (NoopClient) Close()        {}
