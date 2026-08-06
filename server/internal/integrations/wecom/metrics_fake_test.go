package wecom

import "sync"

// recordingMetrics is the package-wide WecomMetrics test double. It counts
// every observation so a test can assert not just "the code ran" but "the code
// reported what happened" — the gap that let a dead realtime outbound path
// stay invisible.
type recordingMetrics struct {
	mu sync.Mutex

	connectFailures    int
	authFailures       int
	welcomeSkipped     int
	welcomeFailures    int
	enqueued           map[string]int // "path/source_kind" -> count
	deliveries         map[string]int // outcome -> count
	installResults     map[string]int // install terminal result -> count
	reconcileRaceLosts int

	// onWelcomeSkipped, when set, fires in addition to the counter. Kept for
	// tests that want to observe ordering rather than totals.
	onWelcomeSkipped func()
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{
		enqueued:       map[string]int{},
		deliveries:     map[string]int{},
		installResults: map[string]int{},
	}
}

func (m *recordingMetrics) RecordConnectFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectFailures++
}

func (m *recordingMetrics) RecordAuthFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authFailures++
}

func (m *recordingMetrics) RecordWelcomeSkippedNonSingle() {
	m.mu.Lock()
	m.welcomeSkipped++
	hook := m.onWelcomeSkipped
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (m *recordingMetrics) RecordWelcomeFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.welcomeFailures++
}

func (m *recordingMetrics) RecordOutboundEnqueued(path, sourceKind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enqueued == nil {
		m.enqueued = map[string]int{}
	}
	m.enqueued[path+"/"+sourceKind]++
}

func (m *recordingMetrics) RecordOutboundDelivery(outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deliveries == nil {
		m.deliveries = map[string]int{}
	}
	m.deliveries[outcome]++
}

func (m *recordingMetrics) RecordReconcileRaceLost() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconcileRaceLosts++
}

func (m *recordingMetrics) RecordInstallSessionTerminal(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.installResults == nil {
		m.installResults = map[string]int{}
	}
	m.installResults[result]++
}

func (m *recordingMetrics) installTerminals(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installResults[result]
}

func (m *recordingMetrics) enqueues(path, sourceKind string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enqueued[path+"/"+sourceKind]
}

func (m *recordingMetrics) delivered(outcome string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deliveries[outcome]
}

func (m *recordingMetrics) raceLosses() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reconcileRaceLosts
}
