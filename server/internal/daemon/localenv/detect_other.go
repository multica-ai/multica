//go:build !darwin && !linux

package localenv

import "context"

func listListeners(context.Context) ([]Listener, error) { return nil, nil }

func inspectProcesses(context.Context, []int) map[int]Process { return nil }

func focusApp(context.Context, int, string) error { return ErrFocusUnsupported }
