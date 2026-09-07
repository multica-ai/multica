package main

import (
	"slices"
	"testing"
)

// The order teardown runs in is a contract, and one link in it is the kind that
// a comment can claim while the code does the opposite: the WeCom cross-replica
// dispatcher delivers over sockets the channel supervisor owns, each supervised
// connection clears its sender as it exits, and the supervisor starts exiting
// the moment its context is cancelled — not when it is joined.
//
// So "relay before supervisor" has to mean "relay before the CANCEL", and this
// pins it against the same type main() runs.
func TestShutdownSequence_RunsInTheDocumentedOrder(t *testing.T) {
	var ran []string
	record := func(name string) func() {
		return func() { ran = append(ran, name) }
	}
	shutdownSequence{
		StopAutopilot:         record("autopilot"),
		DrainHTTP:             record("http"),
		StopOutboundRelay:     record("relay"),
		CancelWorkers:         record("cancel"),
		StopHeartbeats:        record("heartbeats"),
		JoinWebhookWorker:     record("webhooks"),
		JoinTelegram:          record("telegram"),
		JoinChannelSupervisor: record("supervisor"),
		DrainChannelRouter:    record("router"),
		StopMetricsServer:     record("metrics"),
		StopProfiling:         record("pprof"),
	}.run()

	want := []string{
		"autopilot", "http", "relay", "cancel", "heartbeats",
		"webhooks", "telegram", "supervisor", "router", "metrics", "pprof",
	}
	if !slices.Equal(ran, want) {
		t.Fatalf("shutdown order = %v, want %v", ran, want)
	}
}

// The one ordering with a user-visible consequence, asserted on its own so a
// future reshuffle fails with the reason attached rather than as a diff of two
// long lists.
func TestShutdownSequence_DrainsTheOutboundRelayBeforeCancellingTheWorkers(t *testing.T) {
	var ran []string
	shutdownSequence{
		StopOutboundRelay: func() { ran = append(ran, "relay") },
		CancelWorkers:     func() { ran = append(ran, "cancel") },
	}.run()

	relay := slices.Index(ran, "relay")
	cancel := slices.Index(ran, "cancel")
	if relay < 0 || cancel < 0 {
		t.Fatalf("both steps must run, got %v", ran)
	}
	if relay > cancel {
		t.Fatal("the relay drain runs after the worker cancel — by then every supervised " +
			"connection has cleared its sender, so the drain finds no socket for any " +
			"installation and discards the replies it exists to save")
	}
}

// A deployment without channels, without a metrics server, or without a
// Telegram worker leaves those steps nil. Skipping them must not skip the rest.
func TestShutdownSequence_SkipsTheStepsADeploymentDoesNotHave(t *testing.T) {
	var ran []string
	shutdownSequence{
		DrainHTTP:     func() { ran = append(ran, "http") },
		CancelWorkers: func() { ran = append(ran, "cancel") },
		StopProfiling: func() { ran = append(ran, "pprof") },
	}.run()

	if want := []string{"http", "cancel", "pprof"}; !slices.Equal(ran, want) {
		t.Fatalf("shutdown order = %v, want %v", ran, want)
	}
}
