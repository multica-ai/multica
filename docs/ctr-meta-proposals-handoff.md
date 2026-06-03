# CTR Meta Proposals Handoff

This is the integration contract for FIR-1946. The Supabase migration lives in
`supabase/migrations/20260527120500_ctr_meta_proposals.sql`.

## Tables

`public.meta_proposals` stores one candidate meta-title/meta-description change
per URL with GSC baseline metrics, model rationale, confidence and approval
state.

Required writer fields for the analysis agent:

- `url`
- `impact_score`
- `hypothesis`
- `proposed_meta_title`
- `proposed_meta_description`
- `rationale`
- `confidence_score`
- `gsc_impressions`
- `gsc_clicks`
- `gsc_ctr`
- `gsc_position`

Optional writer fields:

- `current_meta_title`
- `current_meta_description`
- `source`
- `analysis_run_id`
- `metadata`

`public.meta_proposal_audit_log` is append-only from triggers. Every insert and
update on `meta_proposals` creates an audit event with actor, role, previous
status, new status and changed approval/proposal fields.

## Access Model

The analysis agent and debate-panel should insert proposals with the Supabase
service role key from backend/agent runtime only. The service role must never be
used in the browser.

Approval UI uses an authenticated Supabase session:

- `GET /rest/v1/meta_proposals?approval_status=eq.pending&order=impact_score.desc`
- `PATCH /rest/v1/meta_proposals?id=eq.<proposal_id>`
- `GET /rest/v1/meta_proposal_audit_log?proposal_id=eq.<proposal_id>&order=created_at.desc`

The UI PATCH body should contain only:

```json
{ "approval_status": "approved" }
```

or:

```json
{ "approval_status": "rejected" }
```

`reviewed_at`, `reviewed_by` and `updated_at` are maintained by database
triggers. RLS allows authenticated users to read proposals/audit rows and update
only `approval_status`; insert is reserved for `service_role`.

## Example Insert

```json
{
  "url": "https://example.com/products/fish-oil",
  "impact_score": 42.75,
  "hypothesis": "High impressions and below-category CTR indicate the SERP copy is underperforming.",
  "current_meta_title": "Fish Oil - Example",
  "current_meta_description": "Buy fish oil online.",
  "proposed_meta_title": "Fish Oil With Omega-3 - Fast Delivery",
  "proposed_meta_description": "Compare quality fish oil supplements and order omega-3 with fast delivery.",
  "rationale": "Adds the primary intent term and clearer delivery/value proposition while staying concise.",
  "confidence_score": 0.78,
  "gsc_impressions": 12000,
  "gsc_clicks": 240,
  "gsc_ctr": 0.02,
  "gsc_position": 7.3,
  "analysis_run_id": "gsc-2026-05-27",
  "metadata": {
    "query_cluster": "omega 3",
    "source": "gsc"
  }
}
```
