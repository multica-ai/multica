package kagentbroker

// These bounds include replay tombstones and ambiguous attempts, not just
// returned tasks. No record is evicted without a durable recovery/ack protocol.
const bindingLimit = 1024

func (b *Broker) authorizeRPCLocked(r map[string]any) int {
	if r["method"] != "SendMessage" {
		if b.tasks[str(object(r["params"])["id"])] == "" {
			return 403
		}
		return 0
	}
	m := object(object(r["params"])["message"])
	c := str(m["contextId"])
	if c != "" && !b.contexts[c] {
		return 403
	}
	if b.sendBlocked || len(b.attempts) >= bindingLimit {
		return 403
	}
	if b.attempts[str(m["messageId"])] || (c != "" && b.busy[c]) {
		return 409
	}
	return 0
}

func (b *Broker) authorizeRPC(r map[string]any) int {
	b.mu.Lock()
	status := b.authorizeRPCLocked(r)
	b.mu.Unlock()
	if status != 0 || r["method"] != "SendMessage" {
		return status
	}
	return b.journal.allowed(str(object(object(r["params"])["message"])["messageId"]))
}

// Reserve after preflight, immediately before the single dispatch. No mutex is
// held during authentication, network I/O, parsing, or writes to the caller.
func (b *Broker) beginSend(r map[string]any) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if status := b.authorizeRPCLocked(r); status != 0 {
		return status
	}
	m := object(object(r["params"])["message"])
	b.attempts[str(m["messageId"])] = true
	if c := str(m["contextId"]); c != "" {
		b.busy[c] = true
	}
	return 0
}

func (b *Broker) finishSend(r, response map[string]any) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := object(object(r["params"])["message"])
	c := str(m["contextId"])
	t := object(object(response["result"])["task"])
	id, returned := str(t["id"]), str(t["contextId"])
	if response["jsonrpc"] != "2.0" || response["id"] != r["id"] || response["error"] != nil ||
		!uuidPattern.MatchString(id) || !uuidPattern.MatchString(returned) ||
		(c != "" && c != returned) || (b.tasks[id] != "" && b.tasks[id] != returned) ||
		(c == "" && b.contexts[returned]) {
		// An unknown session may now exist. With no durable message lookup or
		// proof of nonacceptance, fail closed for all further sends.
		b.sendBlocked = true
		return false
	}
	b.tasks[id] = returned
	// Only a fully validated response releases this session for another turn.
	b.contexts[returned] = true
	b.busy[returned] = true
	return true
}

func (b *Broker) boundResponse(r, response map[string]any) bool {
	t := object(response["result"])
	if r["method"] == "SendMessage" {
		t = object(t["task"])
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tasks[str(t["id"])] != "" && b.tasks[str(t["id"])] == t["contextId"]
}

func (b *Broker) releaseSend(r, response map[string]any) {
	if r["method"] != "SendMessage" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.busy, str(object(object(response["result"])["task"])["contextId"]))
}

// No upstream call took place; release only the local reservation. A possibly
// written durable tombstone remains authoritative even on disk/cancel errors.
func (b *Broker) abortSend(r map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := object(object(r["params"])["message"])
	delete(b.attempts, str(m["messageId"]))
	delete(b.busy, str(m["contextId"]))
}
