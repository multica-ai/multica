package daemon

func providerCapabilities(providerType, executable string) map[string]any {
	return cerebroProviderCapabilities(providerType, executable) // CEREBRO-PATCH(daemon-capability-probe): keep the upstream hook thin.
}
