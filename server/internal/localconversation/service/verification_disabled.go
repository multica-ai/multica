//go:build !verificationdev

package service

import (
	"context"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

func (s *Service) verificationRunner(context.Context, sqlitedb.Conversation) (runner, bool, error) {
	return nil, false, nil
}
func (s *Service) verificationMode() bool { return false }
func (s *Service) verificationClose()     {}

func verificationOptions(opts Options) Options { return opts }
