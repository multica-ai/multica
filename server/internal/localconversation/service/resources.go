package service

import "time"

// Limits on processes kept running for conversations that are not running a
// turn, and on the work done to prepare them.
const (
	// prepareDeadline bounds one prepare task: process start, handshake,
	// configuration and thread or Query setup. It stays below sendDeadline so
	// a send that joined a hung preheat can still report the failure itself.
	prepareDeadline = 4 * time.Minute

	// maxIdleProcesses is how many idle conversation processes may stay, not
	// counting spares and running conversations; focused ones count but stay.
	maxIdleProcesses = 4
	// preheatedIdleLimit applies to a process prepared ahead of a send that
	// never came; it is also evicted first when over maxIdleProcesses.
	preheatedIdleLimit = 5 * time.Minute
	// sentIdleLimit applies to a process that has served a turn.
	sentIdleLimit = 30 * time.Minute

	// focusGrace keeps a connection's focus after it closes, so moving to a
	// direct connection or a short drop is not treated as leaving.
	focusGrace = 30 * time.Second

	// maintenanceInterval is how often grace periods, idle limits and spares
	// are checked.
	maintenanceInterval = 5 * time.Second

	// spareMaxAge bounds what a parked process has frozen, such as Claude's
	// authentication, user skills and plugins.
	spareMaxAge = 30 * time.Minute
	// spareNoClientGrace keeps a spare through a short gap between clients.
	spareNoClientGrace = focusGrace
	// spareCheckInterval is how often a provider's readiness is read while no
	// claim needs it now.
	spareCheckInterval = 30 * time.Second
	// spareHealthyAfter is how long a spare must live for its start to count
	// as a success; one that exits sooner counts as a failed start.
	spareHealthyAfter = time.Minute
	// spareBackoffBase doubles with each consecutive failed start.
	spareBackoffBase = 5 * time.Second
	// spareStaleSteadyAfter is how long a spare must stay valid for its discard
	// to count as an isolated change rather than churn.
	spareStaleSteadyAfter = 10 * time.Minute
	// spareStaleBackoffBase doubles with each replacement in a row that went stale
	// sooner than spareStaleSteadyAfter, up to spareMaxAge.
	spareStaleBackoffBase = time.Minute
	// spareMaxFailures consecutive failed starts disable a provider's spares
	// until its executable changes.
	spareMaxFailures = 5
)
