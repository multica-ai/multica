package channel

var providers = map[string]Provider{
	"slack": &SlackProvider{},
}

// GetProvider returns the provider implementation for the given name.
func GetProvider(name string) (Provider, bool) {
	p, ok := providers[name]
	return p, ok
}
