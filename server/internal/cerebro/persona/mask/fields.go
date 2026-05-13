package mask

// ResourceKind is the persona resource_kind string Multica uses for
// each entity. Persona's grant policies key on these strings, so
// changing them is a breaking change for any operator who has issued
// classification-bearing grants.
const (
	KindMember     = "multica.member"
	KindUser       = "multica.user"
	KindAgent      = "multica.agent"
	KindIssue      = "multica.issue"
	KindComment    = "multica.comment"
	KindAttachment = "multica.attachment"
)

// staticFieldClassifications holds per-kind, per-field labels for
// resources whose sensitivity is inherent to the field — email is
// always PII regardless of the row, name is always internal, etc.
// Resources with per-row classification (issue/comment/attachment)
// still go through here for any *additional* per-field labels they
// carry, but in v1 they have none — the per-row label is the only
// signal.
//
// To add a new field-level label, add the entry here. The mask layer
// passes the kind's map to persona's CheckResourceWithMasking as the
// field_classifications attribute; persona then matches per-field
// classification against the firing grant's MaxClassification.
//
// v1 scope (per Sara's 23:04 scope precision):
//   - members.email = pii
//   - members.phone = pii
//   - users.email   = pii
//   - users.phone   = pii
//
// agent and per-row-classified kinds are intentionally empty maps.
var staticFieldClassifications = map[string]map[string]string{
	KindMember: {
		"email": "pii",
		"phone": "pii",
	},
	KindUser: {
		"email": "pii",
		"phone": "pii",
	},
	KindAgent:      {},
	KindIssue:      {},
	KindComment:    {},
	KindAttachment: {},
}

// FieldClassifications returns the static per-field labels for a
// resource kind, or nil when the kind has no per-field labels. The
// returned map is a fresh copy so callers can safely mutate it (e.g.
// to inject a per-row override) without polluting the package
// constant.
func FieldClassifications(kind string) map[string]string {
	src, ok := staticFieldClassifications[kind]
	if !ok || len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// ApplyToJSON walks a JSON-shaped map and replaces every value at one
// of the given field paths with nil. Used by handlers right before
// json.Marshal: load the row, build the JSON shape, then call this to
// strip the masked fields.
//
// Path semantics: a simple top-level field name ("email") matches
// payload["email"]. Nested paths are not supported in v1 — the static
// map only labels top-level fields and per-row classification covers
// the whole row, so a nested path would mean a future per-field
// override on a JSONB column. Left for v2.
func ApplyToJSON(payload map[string]any, fields []string) {
	if len(payload) == 0 || len(fields) == 0 {
		return
	}
	for _, f := range fields {
		if _, present := payload[f]; present {
			payload[f] = nil
		}
	}
}
