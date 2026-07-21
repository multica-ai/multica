package availabilityevidence

import (
	"context"
	"fmt"
	"time"
)

// Surface is one runtime's live capability listing. It answers what the runtime
// actually exposes right now, not what configuration says it should expose. A
// Surface must read the same registry the runtime executes from — a Surface that
// reads the config it is meant to check would only confirm the config.
type Surface interface {
	RuntimeType() RuntimeType
	// Present reports whether the runtime exposes the canonical capability.
	Present(ctx context.Context, capabilityID string) (bool, error)
}

// Gate answers whether a subject may call a capability. The probe calls it for
// both an authorized and an unauthorized subject to prove the gate works in both
// directions. Decide MUST be read-only: it reports the verdict the gate would
// reach, and never performs the call or mutates anything.
type Gate interface {
	Decide(ctx context.Context, capabilityID string, subject Subject) (Outcome, error)
}

// Prober produces evidence for a set of canonical capabilities on one runtime.
// Gate may be nil: the probe then reports what it found (discovered) and never
// claims a verified capability it could not prove.
type Prober struct {
	Surface Surface
	Gate    Gate
	// Now supplies proof timestamps. Nil uses time.Now.
	Now func() time.Time
}

// Probe observes every capability on the prober's runtime and records the
// evidence in the ledger. A capability the surface cannot answer for is recorded
// as declared with the error as its reason — a probe failure never silently
// upgrades or downgrades a capability, and never looks like a clean absence.
func (p Prober) Probe(ctx context.Context, ledger *Ledger, capabilityIDs []string, authorized, unauthorized Subject) error {
	if p.Surface == nil {
		return fmt.Errorf("availabilityevidence: probe requires a surface")
	}
	rt := p.Surface.RuntimeType()

	for _, id := range capabilityIDs {
		present, err := p.Surface.Present(ctx, id)
		if err != nil {
			ledger.record(Evidence{
				CapabilityID: id,
				RuntimeType:  rt,
				Level:        LevelDeclared,
				Reason:       fmt.Sprintf("the runtime surface could not be read: %v", err),
			})
			continue
		}

		o := Observation{CapabilityID: id, RuntimeType: rt, Present: present}
		if present && p.Gate != nil {
			for _, subject := range []Subject{authorized, unauthorized} {
				proof, err := p.prove(ctx, id, subject)
				if err != nil {
					continue
				}
				o.Proofs = append(o.Proofs, proof)
			}
		}
		ledger.Record(o)
	}
	return nil
}

func (p Prober) prove(ctx context.Context, capabilityID string, subject Subject) (Proof, error) {
	outcome, err := p.Gate.Decide(ctx, capabilityID, subject)
	if err != nil {
		return Proof{}, err
	}
	return Proof{
		Subject:    subject,
		Outcome:    outcome,
		Detail:     fmt.Sprintf("gate returned %s for %s", outcome, subject.Name),
		ObservedAt: p.now(),
	}, nil
}

func (p Prober) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

// record stores pre-classified evidence. Used for probe failures, where there is
// no observation to classify.
func (l *Ledger) record(e Evidence) {
	if l.entries == nil {
		l.entries = make(map[ledgerKey]Evidence)
	}
	l.entries[ledgerKey{capability: e.CapabilityID, runtime: e.RuntimeType}] = e
}

// StaticSurface is a Surface backed by an in-memory name set. It is how a
// runtime whose tool list is known in-process (the Gateway registry, the MCP
// tool server) is probed without a network hop.
type StaticSurface struct {
	Type       RuntimeType
	Capability map[string]bool
}

// NewStaticSurface builds a surface from the canonical capability IDs a runtime
// exposes.
func NewStaticSurface(rt RuntimeType, capabilityIDs []string) *StaticSurface {
	set := make(map[string]bool, len(capabilityIDs))
	for _, id := range capabilityIDs {
		set[id] = true
	}
	return &StaticSurface{Type: rt, Capability: set}
}

// RuntimeType implements Surface.
func (s *StaticSurface) RuntimeType() RuntimeType { return s.Type }

// Present implements Surface.
func (s *StaticSurface) Present(_ context.Context, capabilityID string) (bool, error) {
	return s.Capability[capabilityID], nil
}
