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

### Appearance

**Seed**:
The few colors a theme names before derivation: accent, foundation, and ink. Components never read seeds.
_Avoid_: token (when meaning the starting colors), surface (the seed is foundation, not `--surface`)

**Token**:
A CSS custom property the UI reads (`--app-shell`, `--page-canvas`, `--brand`, `--primary`). Names stay; values may be derived from seeds.
_Avoid_: workplane, elevated, accent (as CSS names — those are another product's roles)

**Foundation**:
The window-level seed that derivation turns into `--app-shell`, `--page-canvas`, and the rest of the surface ladder. Not the `--surface` token.
_Avoid_: surface (unqualified)

**Skin**:
One light/dark pair of seeds. Multica’s product skin uses the Linear catalog seeds (accent, foundation, ink, contrast); the derivation algorithm is ours, not Linear’s static palette.
_Avoid_: theme (when that would mean next-themes light/dark only)

## Flagged ambiguities

**Surface**: `--surface` is the card fill on the page canvas. The appearance seed for the window is **foundation**. Do not call the seed "surface".

## Example

"On a wide web window, ⌘-click opens a session tab. On compact, the same click is a browser tab. Native mobile just pushes the issue onto the stack."

"Keep the token names; change the values. `--surface` is a card, not the foundation seed. The skin can look more Linear; the derivation algorithm stays ours."
