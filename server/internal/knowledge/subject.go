package knowledge

import "context"

type subjectContextKey struct{}

// Subject describes the authentication provenance of a knowledge request.
// Human requests default to private access. A task-token request must be
// positively proven to originate from the token owner's personal chat before
// it receives that same access; otherwise it can still read workspace-shared
// bases but not private ones.
type Subject struct {
	TaskToken     bool
	PrivateAccess bool
}

func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, subjectContextKey{}, subject)
}

func SubjectFromContext(ctx context.Context) (Subject, bool) {
	subject, ok := ctx.Value(subjectContextKey{}).(Subject)
	return subject, ok
}

func privateAccessAllowed(ctx context.Context) bool {
	subject, ok := SubjectFromContext(ctx)
	if !ok {
		return true
	}
	return subject.PrivateAccess
}
