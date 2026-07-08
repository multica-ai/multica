package featureflag

import "hash/fnv"

// bucketFor returns a deterministic bucket in [0, 100) for the supplied
// (key, identifier) pair using FNV-1a. The same pair always returns the
// same bucket, which is the contract callers rely on for stable percent
// rollouts: a user must not flip in and out of an experiment across
// requests.
//
// FNV-1a is used instead of crypto hashes because it is fast, dependency
// free, and well-distributed enough for sub-100 bucketing. The hash is not
// security sensitive; do not use it for anything beyond bucketing.
func bucketFor(key, identifier string) int {
	h := fnv.New32a()
	// Writing each component with a separator avoids a "key||identifier"
	// collision pattern where ("ab", "c") and ("a", "bc") would hash to
	// the same value.
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(identifier))
	return int(h.Sum32() % 100)
}

// inPercent reports whether (key, identifier) falls within the first
// percent buckets. A percent of 0 disables the rule for everyone; a
// percent of 100 enables it for everyone. Values outside [0, 100] are
// clamped.
func inPercent(key, identifier string, percent int) bool {
	switch {
	case percent <= 0:
		return false
	case percent >= 100:
		return true
	}
	return bucketFor(key, identifier) < percent
}
