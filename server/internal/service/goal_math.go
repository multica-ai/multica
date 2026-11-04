package service

import (
	"math"
	"time"
)

// Goal arithmetic (spec 4.1). These functions hold the rules and touch no
// database, so they are tested as tables. The service turns stored rows into
// their inputs and writes their outputs.

// Confidence values, as the goal table's CHECK allows them.
const (
	ConfidenceOnTrack  = "on_track"
	ConfidenceAtRisk   = "at_risk"
	ConfidenceOffTrack = "off_track"
	ConfidenceUnknown  = "unknown"
)

// Confidence bands, in fractions of progress below the expected share.
const (
	ConfidenceOnTrackBand = 0.10
	ConfidenceAtRiskBand  = 0.30
)

// progressEpsilon absorbs floating-point error at the band edges, so a goal
// exactly ten points behind is on the at-risk side of the line, not an error.
const progressEpsilon = 1e-9

// KeyResultProgress is clamp((current - start) / (target - start), 0, 1). The
// sign of the difference carries the direction: a key result that should fall
// has a target below its start, and a fall gives a positive progress.
func KeyResultProgress(start, target, current float64) float64 {
	span := target - start
	if span == 0 || math.IsNaN(span) || math.IsNaN(current) {
		return 0
	}
	return clamp01((current - start) / span)
}

// ExpectedShare is the fraction of a goal's period that has passed at now: a
// straight line from its start (0) to its end (1).
func ExpectedShare(periodStart, periodEnd, now time.Time) float64 {
	total := periodEnd.Sub(periodStart)
	if total <= 0 {
		return 1
	}
	return clamp01(float64(now.Sub(periodStart)) / float64(total))
}

// KeyResultState is one key result's input to the goal's progress: its progress
// when it has a reading in the period, and its weight (1 to 10).
type KeyResultState struct {
	Progress float64
	Weight   int
	HasRead  bool
}

// GoalProgress is the weighted mean of the progress of the key results that
// have a reading in the period. ok is false when none has one, so there is no
// progress to report rather than a zero.
func GoalProgress(krs []KeyResultState) (progress float64, ok bool) {
	var sum, weights float64
	for _, kr := range krs {
		if !kr.HasRead || kr.Weight <= 0 {
			continue
		}
		sum += kr.Progress * float64(kr.Weight)
		weights += float64(kr.Weight)
	}
	if weights == 0 {
		return 0, false
	}
	return sum / weights, true
}

// DeriveConfidence sets a goal's confidence against the expected share. A goal
// with any key result still without a reading in its period is unknown: the
// line cannot be drawn for a number nobody has reported. Otherwise progress at
// or above the expected share minus ten points is on track, at or above minus
// thirty is at risk, and anything less is off track.
func DeriveConfidence(progress, expected float64, allRead bool) string {
	if !allRead {
		return ConfidenceUnknown
	}
	switch {
	case progress+progressEpsilon >= expected-ConfidenceOnTrackBand:
		return ConfidenceOnTrack
	case progress+progressEpsilon >= expected-ConfidenceAtRiskBand:
		return ConfidenceAtRisk
	default:
		return ConfidenceOffTrack
	}
}

// EffectiveConfidence is the owner's override when there is one, otherwise the
// derived value. An override is shown as such by the caller.
func EffectiveConfidence(derived string, override *string) string {
	if override != nil && *override != "" {
		return *override
	}
	return derived
}

func clamp01(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
