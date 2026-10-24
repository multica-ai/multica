# Description-bearing workflow handoff cases

Behavioral reference: [Workflow handoffs](../../../apps/docs/content/docs/issues.mdx#workflow-handoffs-with-saved-descriptions).

| Case | Expected saved state and dispatch | Automated coverage |
| --- | --- | --- |
| Save description and enter a target status, Agent A currently assigned | Description saved, Agent B assigned by the workflow, one Agent B run, no Agent A run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Batch description/status handoff | Same outcome for batch write | TestBatchDescriptionUpdatePreservesWorkflowHandoff |
| Explicit member assignee with description/status | Member retained, no configured agent run | TestDescriptionHandoffExplicitMemberOverride |
| Retry unchanged status with description | No second Agent B run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Untouched assignee refresh | Existing nullable-field merge behavior remains | TestRefreshUntouchedNullableIssueParamsKeepsValidatedAssigneePair |

The workflow-selected agent sees the saved description without a second client action. Server-crash/outbox recovery is outside this change.
