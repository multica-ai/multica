package tagaccess

// FixtureDeliveryVerifier is available only while compiling tagaccess tests.
// Production callers cannot construct a Gate with a substitute verifier.
type FixtureDeliveryVerifier = fixtureDeliveryVerifier

func NewFixtureDeliveryVerifier() *FixtureDeliveryVerifier {
	return newFixtureDeliveryVerifier()
}

func New(adapter store, clock Clock, verifier *FixtureDeliveryVerifier) *Gate {
	return newGate(adapter, clock, verifier)
}
