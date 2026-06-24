# TC-214: Inbox 进频道可后退 + 消息定位（OPE-3457 #2）

Purpose: Verify that entering a channel from the Inbox supports browser-back navigation (returns to Inbox), and that a `?message=<id>` deep-link correctly locates both top-level messages and replies (replies require the thread panel to auto-open and the reply to be highlighted).

Preconditions: The Multica web app is reachable on the PR build. The user is signed in. An Inbox item pointing to a channel message exists (a top-level message and a reply, each with a known id).

User flow:
1. Open the Inbox. Click a channel-message inbox item.
2. Verify navigation lands on the channel page (not a replace that wipes history).
3. Press browser back. Verify it returns to the Inbox (not "stuck" with no way back).
4. Navigate to the channel with `?message=<topLevelId>`. Verify the top-level message is scrolled into view and briefly highlighted.
5. Navigate to the channel with `?message=<replyId>` (a reply id). Verify the thread root is centered, the reply panel auto-opens, and the reply is scrolled into view and highlighted inside the panel.

Expected results: Inbox → channel supports back navigation. Top-level `?message` locates + highlights the message. Reply `?message` centers the thread root, auto-opens the reply panel, and highlights the reply (`#channel-reply-${id}` with `ring-2 ring-brand/50`).

Notes for automation: Inbox uses `push` (not `replace`) so history is preserved. Reply deep-link goes through `?around` resolution server-side → response carries `highlight{root_message_id,thread_id,message_id}` → frontend auto-opens replies via `onOpenReplies` and the `RepliesPanel` highlights `#channel-reply-${id}`. Back-navigation is a real history stack assertion, not a route replace.
