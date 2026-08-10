//go:build !wecomfaults

package wecom

// faults_off.go — what a shipped binary has instead of fault injection:
// nothing.
//
// This is the half of the pair that is compiled by default. Every function here
// returns the zero answer and does no work, which the compiler inlines and then
// deletes: the `if` in writeLocked reduces to `if nil != nil` and leaves no
// branch behind it. A runtime switch could not offer that — it would put a real
// test on every frame this adapter writes, and, worse, it would mean a
// production binary could be made to drop frames by setting an environment
// variable. Turning the failure paths on has to take a deliberate rebuild:
//
//	cd server && go build -tags wecomfaults ./cmd/server
//
// faults.go is the other half and says what the faults are and how to arm one.

import "log/slog"

// SetFaults exists in both builds so the wiring does not have to care which one
// it is in. Here it arms nothing and says so by returning nothing, which is what
// lets router.go tell an operator that the binary they are running has no fault
// injection compiled into it.
func SetFaults(string) []string { return nil }

// faultDeadSocketOnSeal is where the tagged build notices a closing frame.
func faultDeadSocketOnSeal(*slog.Logger, bool) {}

// faultDeadSocketRefusesWrite is where the tagged build fails a write.
func faultDeadSocketRefusesWrite() error { return nil }
