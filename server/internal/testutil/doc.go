// Package testutil holds the fixtures the server's DB-backed tests build
// their scenarios from: row builders that insert a fixture row and delete it
// again when the test ends, and HTTP helpers that run one handler and assert
// on what it wrote.
//
// It exists because the cost of changing shared behaviour was scaling with
// the number of test files. Before this package `internal/handler` alone held
// ~1000 hand-written `INSERT INTO ... RETURNING id` blocks, ~1150
// `DELETE FROM` cleanups and ~1200 copies of the
// recorder/call/status/decode quartet, each one an independent statement of
// the same contract. Changing an insert's column set, or the shape of a
// failure message, meant editing every copy.
//
// Two rules keep it that way:
//
//   - New DB-backed tests build their rows through a Fixture and drive
//     handlers through Call. Do not open-code an INSERT + t.Cleanup pair or a
//     recorder/status/decode quartet in a new test.
//   - Helpers here stay behaviour-free. They insert rows, run handlers and
//     report mismatches; they never assert a product rule on the test's
//     behalf. A helper that knows what a correct response looks like moves
//     the assertion out of the test that is supposed to make it.
//
// The package imports "testing" and must only be imported from _test files.
// Importing it from production code links the testing flag set into the
// binary.
package testutil
