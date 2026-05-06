package localmode

// MaxConcurrentTasks caps the number of active (running + dispatched) tasks
// across all agents in a workspace when running in local product mode. This
// prevents a single laptop from overcommitting itself when several agents
// are all in working state. The number is intentionally small — local users
// have 1 host with finite RAM/CPU.
const MaxConcurrentTasks = 2
