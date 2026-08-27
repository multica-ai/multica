# Multica

AI-native task management for small teams. Agents are first-class assignees.

## Language

**Session tab**:
An in-app document tab: one workspace-scoped URL plus its virtual history and view memento. The strip shows the current workspace's session tabs. Only the active session is mounted.
_Avoid_: browser tab, window, route (a session is not a router)

**Browser tab**:
A tab owned by the web browser (or a dedicated Electron issue window). Not a session tab.

**Compact**:
Viewport narrower than 1024px (Tailwind `lg`). Inbox/Chat fold to a single column and the session-tab strip is hidden. This is what "mobile" / "small screen" means for web and desktop chrome.
_Avoid_: mobile (when that would mean the Expo app)

**Native mobile**:
The Expo iOS app. Independent information architecture (bottom bar + stack). It does not host session tabs.
_Avoid_: mobile (unqualified — say native mobile or compact)

## Example

"On a wide web window, ⌘-click opens a session tab. On compact, the same click is a browser tab. Native mobile just pushes the issue onto the stack."
