package main

// shutdown.go — the order teardown runs in, as named steps rather than as the
// order statements happen to appear in main().
//
// It is a type because one of the orderings is load-bearing and invisible at
// the call site. The WeCom cross-replica dispatcher delivers over the
// WebSockets the channel supervisor owns, and every supervised connection
// clears its sender from the registry as it exits. The supervisor starts
// exiting the moment its context is cancelled — not when it is joined — so
// "stop the relay before joining the supervisor" is not enough: by then the
// registry is already empty and the dispatcher's drain finds no socket for any
// installation and discards everything it exists to save.
//
// So the relay stop has to come before the cancel, and a comment saying so is
// exactly the kind of claim that quietly stops being true. TestShutdownSequence
// pins it against this type.

// shutdownSequence is every teardown step, in the order it must run. A nil step
// is skipped, which is what lets a deployment without a metrics server or
// without channels use the same sequence.
type shutdownSequence struct {
	// StopAutopilot first: it schedules work the rest of the system performs.
	StopAutopilot func()

	// DrainHTTP before anything that serves it. In-flight handlers finish
	// calling Schedule() before the scheduler stops, and no new task
	// completion can arrive after this — which is what makes the relay stop
	// below a drain rather than a race.
	DrainHTTP func()

	// StopOutboundRelay BEFORE CancelWorkers. See the file header: the
	// dispatcher's drain can only deliver while the supervised sockets are
	// still live, and CancelWorkers is what starts tearing them down.
	StopOutboundRelay func()

	// CancelWorkers cancels the sweeper context every background loop is
	// bound to, including the channel supervisor.
	CancelWorkers func()

	// StopHeartbeats flushes the final batch of queued heartbeat bumps.
	StopHeartbeats func()

	JoinWebhookWorker func()
	JoinTelegram      func()

	// JoinChannelSupervisor waits for the per-installation goroutines so the
	// lease renewer can issue a final release before exit; without it the next
	// replica waits out the whole LeaseTTL after a redeploy.
	JoinChannelSupervisor func()

	// DrainChannelRouter flushes debounced run triggers and joins in-flight
	// outbound replies, now that no supervisor is delivering inbound events.
	DrainChannelRouter func()

	StopMetricsServer func()
	StopProfiling     func()
}

// run performs the sequence. The order here IS the contract.
func (s shutdownSequence) run() {
	for _, step := range []func(){
		s.StopAutopilot,
		s.DrainHTTP,
		s.StopOutboundRelay,
		s.CancelWorkers,
		s.StopHeartbeats,
		s.JoinWebhookWorker,
		s.JoinTelegram,
		s.JoinChannelSupervisor,
		s.DrainChannelRouter,
		s.StopMetricsServer,
		s.StopProfiling,
	} {
		if step != nil {
			step()
		}
	}
}
