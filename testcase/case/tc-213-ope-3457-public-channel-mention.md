# TC-213: 公开频道 @ workspace 任意成员触发通知（OPE-3457 #6）

Purpose: Verify that in an open (public) channel, `@` can mention and notify any workspace member (not restricted to channel members), and that the invite-channel regression is preserved (invite channels still restrict @ candidates to channel members only).

Preconditions: The Multica web app is reachable on the PR build. The user is signed in. A workspace with ≥2 members exists. One open (public) channel and one invite-only channel exist.

User flow:
1. Open the open channel. Type `@` in the composer.
2. Verify the mention candidate list includes ALL workspace members (not just channel members).
3. Pick a workspace member who is NOT a channel member, post the message.
4. Verify the mention resolves (renders as a member mention) and a notification is triggered for that member.
5. Open the invite-only channel. Type `@`.
6. Verify the candidate list is restricted to channel members only (regression: narrowing must not change).

Expected results: Open channel → @ lists all workspace members and notifying a non-member member works. Invite channel → @ lists only channel members (unchanged behavior, no regression).

Notes for automation: The candidate set is computed client-side from `mentionMemberIds` (open = workspace members, invite = channel members). Notification delivery may require the target member to have a reachable notification channel; if real delivery cannot be observed, verify the mention rendering + candidate-list scope as the reachable UI assertions and mark delivery-only confirmation blocked if the binding is absent.
