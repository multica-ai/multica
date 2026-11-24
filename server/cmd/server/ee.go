//go:build !oss

package main

// The commercial features in server/ee. Build with -tags oss (or delete this
// file together with server/ee) for the open-source server.
import _ "github.com/multica-ai/multica/server/ee"
