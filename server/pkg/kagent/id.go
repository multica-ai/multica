package kagent

import (
	"crypto/rand"
	"fmt"
)

// NewID returns a random UUID for durable enrollment and message correlation.
func NewID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("kagent: random source unavailable")
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}
