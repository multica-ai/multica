package taskmandate

// ClaimLifecycleState records whether a claim generation predates generation
// metadata, is still being assembled, or has been finalized for enforcement.
type ClaimLifecycleState string

const (
	ClaimLifecycleLegacy    ClaimLifecycleState = "legacy"
	ClaimLifecycleDraft     ClaimLifecycleState = "draft"
	ClaimLifecycleFinalized ClaimLifecycleState = "finalized"
)

// ClaimGeneration is the additive generation metadata stored beside a task
// mandate. Legacy rows intentionally leave producer, finalizer, versions, and
// digest nil until a later claim is finalized through the generation contract.
type ClaimGeneration struct {
	Generation           int64
	Producer             *string
	Finalizer            *string
	LifecycleState       ClaimLifecycleState
	InventoryVersion     *string
	DiscoveryVersion     *string
	FinalizedGrantDigest *string
}
