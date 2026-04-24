package storage

import "errors"

// ErrPresignUnsupported is returned by storage backends that do not
// implement PresignPut (currently: LocalStorage). Callers should fall
// back to the body-upload path.
var ErrPresignUnsupported = errors.New("storage backend does not support pre-signed uploads")
