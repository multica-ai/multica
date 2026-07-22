package runtime

import "context"

type stubTool struct{ name string }

func (s stubTool) Name() string                                             { return s.name }
func (s stubTool) Description() string                                      { return "stub " + s.name }
func (s stubTool) InputSchema() map[string]any                              { return map[string]any{"type": "object"} }
func (s stubTool) Call(_ context.Context, _ map[string]any) (string, error) { return "", nil }
