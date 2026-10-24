# Description-bearing workflow handoff cases

Behavioral reference: [Issue review handoffs](../../../apps/docs/content/docs/issues.mdx#review-handoffs-with-saved-results).

| Case | Expected saved state and dispatch | Automated coverage |
| --- | --- | --- |
| Save result and enter review, existing engineer assignee | Result saved, configured reviewer assigned, one reviewer run, no engineer run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Batch description/status handoff | Same outcome for batch write | TestBatchDescriptionUpdatePreservesWorkflowHandoff |
| Explicit member reviewer with description/status | Member retained, no configured agent run | TestDescriptionHandoffExplicitMemberOverride |
| Retry unchanged status with result | No second reviewer run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Concurrent untouched assignee refresh | Existing nullable-field merge behavior remains | TestRefreshUntouchedNullableIssueParamsKeepsValidatedAssigneePair |

The reviewer sees the saved result without a second client action. Server-crash/outbox recovery is outside this change.
