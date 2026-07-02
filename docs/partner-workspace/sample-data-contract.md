# Partner Workspace Sample Data Contract

Status: Draft for Milestone 1.

This contract defines the redacted, normalized sample data shape for Partner Workspace V1. It is based on the AI-20 historical outreach issue and the AI-103 active outreach and meeting-prep issue. Raw source material stays outside the repository.

## Source Inventory

### AI-20

AI-20 is the historical index for the original international brand outreach workflow.

Representative source material:

| Source ref | Type | Representative content | Repo treatment |
| --- | --- | --- | --- |
| `multica:issue:AI-20` | manual_note | Original user request, workflow decisions, old metadata, and closure decision moving active work to AI-103. | Use as source reference only. |
| `multica:comment:925fc391-78fc-4fc9-beff-aa23e6267cdd` | manual_note | Wingstop email timeline summary and recommendation. | Normalize into partner history; do not copy raw thread. |
| `gmail:thread:19e6708f96565a63` | email_thread | Wingstop outreach, background responses, review acknowledgement, meeting coordination. | Store only message IDs, summaries, and derived claims. |
| `multica:comment:920aca7f-e0c6-4738-bff4-fa943c8ab45f` | manual_note | Historical brand follow-up list and suggested new brands. | Normalize into partner/action candidates. |
| `multica:comment:14809da4-c222-489f-a8a5-d3a2988962d4` | manual_note | AI-20 closure and migration to AI-103. | Use for issue lineage. |

### AI-103

AI-103 is the active operating issue for brand outreach patrol, reply handling, meeting scheduling, meeting-prep, NDA/platform work, and follow-up.

Representative source material:

| Source ref | Type | Representative content | Repo treatment |
| --- | --- | --- | --- |
| `multica:issue:AI-103` | manual_note | Current operating rules, active partner states, contacts, Gmail IDs, meeting times, waiting_on metadata. | Normalize into partner records and workflow rules. |
| `gmail:thread:19e6cbe11a4f708f` | email_thread | Playa Bowls contact history and meeting coordination. | Store redacted partner state and message IDs only. |
| `multica:comment:8a4968c0-2c06-4e97-be19-a9617a64b694` | email_summary | Patrol found The Halal Guys and GDK inbound updates; includes connector failure case. | Normalize as inputs and evidence without raw body. |
| `multica:comment:a5f7b26e-ccba-4b6e-b435-0fdf10be4f92` | manual_note | GDK reschedule recommendation derived from user screenshots and constraints. | Normalize as chat/manual-note-derived action. |
| `multica:comment:bb642dc1-dbd9-42d0-901e-f49f8eca3af9` | manual_note | Product direction: Partner Workspace is generic, not email-only. | Use as contract evidence. |
| `multica:attachment:019eb0ca-20e2-7758-83fe-b226a83982d6` | attachment | Unified positioning document attached to AI-103. | Reference attachment ID only; raw file is not committed here. |

## Repository Boundary

Allowed in repo:

- Normalized partner records.
- Redacted contact roles and public brand names.
- Source references such as Multica issue IDs, comment IDs, attachment IDs, and Gmail message/thread IDs.
- Derived summaries, stages, actions, and validation fixtures.

Not allowed in repo:

- Raw email bodies.
- Full screenshots or OCR dumps.
- Personal phone numbers, private emails, signatures, calendar links, meeting links, legal documents, or NDA text.
- Unredacted attachments from Multica, Gmail, WeChat, or local files.
- Claims without source references.

## Core Schema

The fixture root is an object with `schema_version`, `source_policy`, and `partners`.

```ts
type SampleDataset = {
  schema_version: "partner-workspace.sample.v1";
  source_policy: {
    raw_source_material_in_repo: false;
    allowed_material: Array<"redacted" | "normalized" | "source_references">;
  };
  partners: PartnerRecord[];
};
```

### Partner

```ts
type PartnerRecord = {
  id: string;
  display_name: string;
  partner_type: "brand" | "vendor" | "landlord" | "supplier" | "agency" | "other";
  origin_issue_refs: SourceRef[];
  current_stage: Stage;
  contacts: Contact[];
  inputs: Input[];
  evidence: Evidence[];
  actions: Action[];
  claims: Claim[];
};
```

### Input

`Input` is any inbound or operator-created material that can change partner understanding. The model must support more than email.

```ts
type Input = {
  id: string;
  input_type: "email" | "meeting_note" | "chat_record" | "attachment" | "manual_note";
  source: InputSource;
  observed_at: string;
  title: string;
  normalized_summary: string;
  redaction_level: "none_public" | "redacted" | "metadata_only";
  evidence_refs: string[];
};
```

### Input Source

```ts
type InputSource = {
  source_type: "gmail" | "multica_issue" | "multica_comment" | "multica_attachment" | "manual_entry" | "meeting_tool" | "chat_tool";
  source_ref: string;
  source_label?: string;
  raw_available_outside_repo: boolean;
};
```

### Evidence

Evidence is the bridge between a normalized claim/action and the source material. Every sample-derived claim must cite one or more evidence IDs.

```ts
type Evidence = {
  id: string;
  source_ref: string;
  evidence_type: "thread_id" | "message_id" | "issue_id" | "comment_id" | "attachment_id" | "operator_note" | "meeting_note";
  supports: string[];
  confidence: "source" | "derived" | "inferred";
};
```

### Stage

```ts
type Stage = {
  id: string;
  label: "research" | "outreach_sent" | "awaiting_reply" | "qualification" | "meeting_scheduled" | "meeting_done" | "legal_or_nda" | "waiting_partner" | "closed";
  summary: string;
  evidence_refs: string[];
};
```

### Action

```ts
type Action = {
  id: string;
  action_type: "draft_reply" | "send_email" | "mark_read" | "schedule_meeting" | "prepare_meeting" | "request_materials" | "review_nda" | "follow_up" | "research" | "no_action";
  status: "suggested" | "needs_human_confirmation" | "approved" | "executed" | "waiting_partner" | "blocked" | "closed";
  title: string;
  rationale: string;
  requires_human_confirmation: boolean;
  due_at?: string;
  evidence_refs: string[];
};
```

### Claim

```ts
type Claim = {
  id: string;
  statement: string;
  claim_type: "partner_fact" | "contact_fact" | "workflow_state" | "user_preference" | "risk" | "recommendation";
  evidence_refs: string[];
};
```

## Normalized Fixture Draft

The companion fixture file is:

- `docs/partner-workspace/fixtures/partner-records.sample.json`

It contains:

- One AI-20-derived partner record: Wingstop historical outreach and meeting coordination.
- One AI-103-derived partner record: Playa Bowls active meeting state.
- A small non-email input coverage set using meeting note, chat record, attachment, and manual note input examples.

## Validation Standard

A sample dataset is valid only when all checks pass:

1. It includes at least one `PartnerRecord` derived from AI-20 and at least one from AI-103.
2. Every `Claim.evidence_refs`, `Stage.evidence_refs`, `Action.evidence_refs`, and `Input.evidence_refs` entry points to an existing evidence object.
3. Every sample-derived claim has at least one source reference. Uncited claims are invalid even when they look obvious.
4. The dataset includes all five input types: `email`, `meeting_note`, `chat_record`, `attachment`, and `manual_note`.
5. The dataset is not email-only: at least two non-email input types must be attached to a partner record that also has an email input.
6. No raw email body, raw screenshot OCR, private meeting link, private calendar link, phone number, private address, or unredacted legal material appears in committed fixtures.
7. Any action with external side effects has `requires_human_confirmation: true`. This includes `send_email`, `mark_read`, `schedule_meeting`, `review_nda`, and commitment-bearing follow-up.
8. Any source that points to raw material has `raw_available_outside_repo: true` and stores only a source reference in the repo.
9. If evidence confidence is `inferred`, the claim or action text must visibly state that it is inferred or recommended, not a fact.
10. Partner model names stay generic. Fixtures may mention real public brand names, but schema fields must not encode a brand-only or email-only workflow.

## Notes for Implementation

- Treat source references as opaque IDs; do not fetch raw material at runtime unless the user grants access through the proper connector/CLI.
- UI should display evidence chips next to generated summaries, stages, and actions.
- Draft generation is allowed in V1, but execution is not automatic. Human confirmation remains part of the data contract.
