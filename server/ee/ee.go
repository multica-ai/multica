// Package ee links the commercial server features into the binary. Each
// subpackage registers itself with package extension from init.
//
// Files under server/ee are not covered by the Multica License; see
// server/ee/LICENSE.
package ee

import (
	_ "github.com/multica-ai/multica/server/ee/projectfolder"
)
