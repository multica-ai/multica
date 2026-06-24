# TC-212: Channel 消息转 Issue 双向关联 + 状态回流（OPE-3457 链路 A #1+#4）

Purpose: Verify that converting a Channel message to an Issue (both the manual route and the Agent/quick-create route) produces structurally isomorphic issues carrying `source_channel_id`/`source_thread_id`, that the two sides are bidirectionally linked (channel message ↔ issue), and that an Issue status change reflows an `OPE-xxx 已完成`-style system message back into the source thread and the channel main timeline — for both conversion paths.

Preconditions: The Multica web app is reachable on the PR build. The user is signed in. A workspace with at least one Channel exists. At least one Agent with an online runtime is available for the Agent-conversion path.

User flow:
1. Open a channel, post a top-level message.
2. Right-click the message → "手工转换 Issue" (manual convert). Fill minimal fields, create.
3. Open the produced issue detail. Verify a "来自频道讨论" badge is shown and is clickable → navigates back to the channel/message.
4. Back in the channel, verify the top-level message now shows a linked-issue chip (e.g. `#NNN title`) that links to the issue.
5. Change the issue status to "已完成/done".
6. Verify the channel thread AND the channel main timeline show a system message like `OPE-xxx 已完成`.
7. Repeat steps 1-6 but via right-click → "通过 Agent 转换 Issue". Verify the Agent-conversion prompt carries the source message content, and the produced issue also has `source_channel_id`/`source_thread_id`, the bidirectional links, and the status reflow all behave identically.

Expected results: Manual and Agent conversion paths produce structurally isomorphic issues (both carry source refs). Channel message shows a clickable linked-issue chip; Issue detail shows a clickable "来自频道讨论" badge and a clickable "mentioned in channel" activity — bidirectional links work. Status change to done reflows an `OPE-xxx 已完成` system message into the source thread and the channel main timeline, for both paths.

Notes for automation: Selectors — top-level message `#channel-msg-${id}`, reply `#channel-reply-${id}`, linked-issue chip strip renders `#NNN title` AppLinks to issue detail, "来自频道讨论" badge is an AppLink to `channelDetail(channel_id)`. Agent-conversion prompt content can be observed via the produced issue description containing a source-message summary. Status reflow is an async WS invalidation — wait for the system message to appear in the thread and the channel timeline.
