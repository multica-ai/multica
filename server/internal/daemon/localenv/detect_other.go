//go:build !darwin && !linux

package localenv

import "context"

func listListeners(context.Context) ([]Listener, error) { return nil, nil }
