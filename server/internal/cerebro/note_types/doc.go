// Package notetypes implements TECH-3511: reusable note templates with
// recurrence, built on top of the artifact feature.
//
// A note type holds a fixed markdown template plus a recurrence behaviour:
//
//   - running_doc: one rolling document; each period a freshly rendered copy
//     of the template is prepended above the previous entries.
//   - new_note:    a fresh note is created per period in the type's folder.
//
// The daily Sweeper materialises scheduled (non-manual) types on cadence; the
// Handler's RunNow endpoint covers manual / off-cycle reviews. Idempotency is
// guaranteed two ways: new_note relies on the unique (note_type_id, period_key)
// index, running_doc compares the current period_key against last_period_key.
package notetypes
