package runtimepool

import (
	"errors"
	"sort"
)

func NormalizeAdvertisedCapabilities(advertised []string) ([]string, error) {
	normalized := append([]string{}, advertised...)
	sort.Strings(normalized)
	out := normalized[:0]
	for _, capability := range normalized {
		if len(capability) > MaxCapabilityBytes || !capabilityNameRE.MatchString(capability) {
			return nil, errors.New("invalid runtime capability")
		}
		if len(out) == 0 || capability != out[len(out)-1] {
			out = append(out, capability)
		}
	}
	if len(out) > MaxCapabilities {
		return nil, errors.New("runtime capabilities exceed 32 unique items")
	}
	return out, nil
}

func ContainsAllCapabilities(have, required []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, capability := range have {
		set[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := set[capability]; !ok {
			return false
		}
	}
	return true
}
