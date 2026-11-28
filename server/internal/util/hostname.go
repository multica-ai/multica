package util

import "regexp"

var dnsHostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// ValidDNSHostname reports whether name is a DNS hostname without a trailing dot.
func ValidDNSHostname(name string) bool {
	return len(name) <= 253 && dnsHostname.MatchString(name)
}
