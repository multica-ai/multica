//go:build !darwin && !linux && !windows

package notifier

import "context"

type platformNotifier struct{}

func New() Notifier {
	return platformNotifier{}
}

func (platformNotifier) Notify(context.Context, string, string) error {
	return nil
}
