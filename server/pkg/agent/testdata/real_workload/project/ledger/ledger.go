// Package ledger parses a plain-text journal and computes account balances.
//
// Each non-blank, non-comment line is one of:
//
//	2026-01-02 deposit  <to>          <amount>
//	2026-01-02 withdraw <from>        <amount>
//	2026-01-02 transfer <from> <to>   <amount>
//
// Amounts are positive with at most two decimals.
package ledger

import (
	"io"
	"time"
)

type Kind string

const (
	Deposit  Kind = "deposit"
	Withdraw Kind = "withdraw"
	Transfer Kind = "transfer"
)

type Txn struct {
	Line     int
	Date     time.Time
	Kind     Kind
	From, To string
	Cents    int64
}

type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { panic("not implemented") }

// Book maps account names to balances in cents.
type Book map[string]int64

type Rejection struct {
	Line   int
	Reason string
}

func Parse(r io.Reader) ([]Txn, error) { panic("not implemented") }

// Apply replays txns in order. A transaction that would overdraw an account,
// or that spends from an account no deposit has created yet, is rejected.
func Apply(txns []Txn) (Book, []Rejection) { panic("not implemented") }

// Report renders balances sorted by account name, followed by a TOTAL line.
func Report(b Book) string { panic("not implemented") }
