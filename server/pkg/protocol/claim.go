package protocol

import "time"

// DaemonBatchClaimRequestTimeout is the existing HTTP client timeout and WS
// execution budget. Servers must leave room inside it for claim recovery and
// response delivery so installed daemons can still receive partial batches.
const DaemonBatchClaimRequestTimeout = 5 * time.Second
