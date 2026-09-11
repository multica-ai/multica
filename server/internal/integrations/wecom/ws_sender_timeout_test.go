package wecom

import "time"

func init() {
	newWSSenderAckTimeout = 400 * time.Millisecond
}
