//go:build !verificationdev

package localhost

import "net/http"

func (v *V2) verificationRoute(http.ResponseWriter, *http.Request, caller, int64, string) bool {
	return false
}
