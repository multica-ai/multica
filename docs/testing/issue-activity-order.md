# Issue activity order test cases

Behavioral source: [Issues — Activity order](../../apps/docs/content/docs/issues.mdx#activity-order).

| Case | Setup and action | Expected outcome | Automated coverage |
| --- | --- | --- | --- |
| API ordering | Request the flat timeline with `direction=asc`, `direction=desc`, no direction, and an invalid direction | Backend returns stable `(created_at, id)` order; default is ascending; invalid values return 400; wrapped legacy requests stay descending | `server/internal/handler/activity_test.go`; `packages/core/api/schema.test.ts` |
| Default and persistence | Open preferences on a new device; select newest first, reload, then select oldest first | Oldest first is the default; either saved choice survives reload | `packages/core/issues/stores/issue-activity-store.test.ts`; `packages/views/settings/components/preferences-tab.test.tsx` |
| Mixed activity | Open an issue with comments, status changes, and an agent run; choose newest first | Main entries and expanded status events read newest first, with separate caches for each server-requested direction | `packages/views/issues/components/issue-detail.test.tsx` |
| Delivered run inputs | Open run activity in either order with the opposite cache absent or stale, then receive a newly delivered message | Delivered input messages remain between the corresponding transcript steps and update live | `packages/views/issues/components/inline-comment-run.test.tsx` |
| Agent input ties | A run covers two messages created in the same second; load either direction | The run stays attached to the latest covered input in both modes | `packages/views/issues/components/comment-runs.test.ts` |
| Threads | Open a comment with replies in newest-first mode | Replies remain under their root in chronological order | `packages/views/issues/components/issue-detail.test.tsx` |
| Live updates | Receive a new comment while reading newest first | The new comment appears before older main entries | `packages/views/issues/components/issue-detail.test.tsx`; `packages/views/issues/hooks/use-issue-timeline.test.tsx` |
| Composer and posting | Use newest first on a narrow viewport and post a comment | Composer is above the activity; the posted comment is located at the top; no jump-to-bottom button is offered | `packages/views/issues/components/issue-detail.test.tsx` |
| Fold/unfold commands | Switch from oldest first to newest first, receive a new or newly resolved root, then use Fold/Unfold All Comments | The commands act on the current visible discussion rather than a stale cache from the other direction | `packages/views/search/search-command.test.tsx` |
| Activity coalescing | View three consecutive same-actor status updates, each less than two minutes apart, in both orders | The same group and count remain, displaying its latest status | `packages/views/issues/components/issue-detail.test.tsx` |
| Navigation | Open a comment deep link or in-page search with newest first | The same reversed order is used in flat and virtualized views; links still locate their target | `packages/views/issues/components/issue-detail.test.tsx` |
| Return to default | Switch back to oldest first | Chronological main entries and bottom composer return immediately | `packages/views/issues/components/issue-detail.test.tsx` |

Manual desktop check: open Preferences, change Issue activity order, then open a long issue and verify recent agent messages and status activity are accessible at the top. Repeat in side preview and a narrow window, and restart the app to check persistence.
