# RNR migration — design, alternatives, and three-phase rollout

**Status**: Phase 0 (research & docs) — complete. Phase 1 (base infrastructure) — not started.

**Audience**: anyone touching `apps/mobile/components/ui/` or adding new UI to the mobile app. Read this once; refer to `apps/mobile/CLAUDE.md` "UI components & theming" for the durable rules.

---

## 1. Why this doc exists

The mobile baseline (`apps/mobile/CLAUDE.md`) has named **react-native-reusables (RNR)** as the shadcn equivalent since SDK 55 was bootstrapped. RNR was never actually installed. As a result:

- `apps/mobile/components/ui/` contains **21 hand-written components, ~1,379 lines**, all built from raw `<View/Text/Pressable/Modal>`.
- `apps/mobile/components/` contains **18 hand-written sheet/modal files**, all copying the same shape (`Modal transparent fade` + hand-drawn backdrop). CLAUDE.md Lesson 6 (lines 250–269) already documents that this pattern is wrong for most content and has produced a series of bugs (keyboard squashing, `maxHeight` clipping FlatLists, `useSafeAreaInsets` returning 0 inside `Modal`).
- There is no dark/light theming infrastructure. `tailwind.config.js` uses hard-coded hex values; `global.css` has only the three `@tailwind` directives; no CSS variables, no `darkMode`, no theme switcher.

This doc records why we are migrating to RNR, what alternatives we evaluated, and how the migration is sequenced.

## 2. Alternatives considered

Evaluated against four criteria: (a) fit with our existing stack (NativeWind 4, Tailwind 3.4, Expo SDK 55, React 19), (b) ownership model (lock-in vs copy-paste), (c) bundler/compiler cost, (d) accessibility baseline.

| Library | Stack fit | Ownership | Build cost | a11y | Verdict |
|---|---|---|---|---|---|
| **react-native-reusables (RNR)** | NativeWind 4 + RN-Primitives + CVA — **identical to ours** | copy-paste, code is yours | zero (just CSS + JSX) | RN-Primitives (focus management, ARIA equivalents) | **Selected** |
| Tamagui | Own compiler + own styled API, not NativeWind | Library lock-in | Babel/bundler config + learning curve | Built-in | Rejected — main value is web+native same-codebase (we don't need it; mobile is independent) |
| Gluestack UI v2 | Own styled API; can be made to work with NativeWind but it's not native | copy-paste possible | low | `@react-native-aria` (strong) | Rejected — switching styling system has no payoff once you already have NativeWind running |
| NativeBase / RN Paper / RN UI Lib | Mature but library-bound | locked | low | varies | Rejected — copy-paste philosophy matters for a long-lived installed app where we may need to patch components without waiting for upstream |

RNR's "components are yours" model also aligns with the desktop/web pattern (`packages/ui/components/ui/` is shadcn copy-paste). One mental model across the codebase.

## 3. Decision — RNR

We adopt RNR with the following commitments. The first two are **principles** that govern every PR in the migration and every UI decision after:

### 3.1 Defaults first

When using any RNR component, accept its default variant, default size, default spacing, default palette. Don't add wrapper layers, "improved" defaults, or `variant="multicaCustom"` styles unless a concrete product need demands it. The hand-written legacy exists precisely because someone reached for a "slightly improved" version of a standard primitive — recreating that pattern with RNR underneath defeats the migration.

Concrete consequences:
- Phase 1 uses shadcn's default neutral palette as-is (light + dark). Multica's existing custom tokens (`brand`, `success`, `warning`, `info`, `priority`, `code-surface`) are appended but **dark-mode values are not authored ahead of demonstrated need** — they copy their light values until a screen using them actually breaks in dark mode.
- Phase 2/3: when `npx @rnr/cli add <component>` writes a file, do not immediately tweak its styles. Use it as-is; adjust callers if API differs.
- Tier C "foundation upgrade" means swap raw `<Text>` for RNR's `Text` and swap inline conditionals for `cva` — it does NOT mean redesign or add variants the component didn't have before.

### 3.2 iOS native > RNR > discuss

When adding a new interaction, walk this waterfall and stop at the first hit:

1. **iOS / RN ships a native API?** Use it directly. Don't wrap a `Modal` to mimic it.
2. **RNR ships a matching component?** `npx @rnr/cli add <name>`. Use default variants.
3. **Neither.** Stop and ask the user. Don't silently hand-roll.

The native-API tier is what §4 Tier B's revised classification depends on — many existing sheets disappear entirely when we use `ActionSheetIOS` / `Alert.prompt` / native datetime picker instead of a custom `Modal`.

| Scenario | Native API |
|---|---|
| Text input prompt (single field) | `Alert.prompt(title, msg, callback)` |
| Confirm / destructive prompt | `Alert.alert` |
| One-of-N action sheet | `ActionSheetIOS.showActionSheetWithOptions` |
| Date / time picker | `@react-native-community/datetimepicker` (installed) |
| Image / camera | `expo-image-picker` (installed) |
| Documents | `expo-document-picker` (installed) |
| Share | `Share.share` from `react-native` |
| Haptics (add to confirm / error / select) | `expo-haptics` (installed) |

### 3.3 Theming

Class-based dark mode (`darkMode: 'class'`) + CSS variables, with an in-app `light` / `dark` / `system` picker stored in `expo-secure-store`. RNR's default Tailwind config uses class mode; this matches our need for an explicit user setting (a media-query-only setup would not let the user override the OS).

### 3.4 Three-tier component classification (see §4)

We are not blanket-migrating every file. Some legacy components are domain UI, not generic primitives; they stay where they are but use RNR's foundation (the `Text` component, semantic tokens, CVA variants).

### 3.5 Hard rule (lives in CLAUDE.md)

**New components come from RNR — or from the native-API tier first.** The migration removes legacy hand-written variants; the rule prevents accumulating more.

## 4. Component inventory & three-tier classification

### Tier A — direct RNR replacement (migrate)

These are generic primitives where RNR ships a near-identical equivalent. Replace the hand-written file with `npx @react-native-reusables/cli@latest add <name>` output, then sweep callers.

| Current file | Lines | RNR component | Notes |
|---|---|---|---|
| `components/ui/button.tsx` | 63 | `button` | Canary for Phase 2 |
| `components/ui/input.tsx` | 9 | `input` | Trivial |
| `components/ui/text-field.tsx` | 34 | `input` + `label` | Composed via RNR |
| `components/ui/card.tsx` | 36 | `card` | Drop-in |
| `components/ui/text.tsx` | 18 | `text` | Used everywhere; do this carefully (sweep all imports) |
| `components/ui/autosize-textarea.tsx` | 89 | `textarea` | Check parity — auto-grow behaviour may need re-implementation on top of RNR's textarea |
| `components/ui/otp-input.tsx` | 68 | (no RNR equivalent today) | Keep using `input-otp-native`, move under Tier B |
| `components/ui/modal-close-button.tsx` | 25 | n/a | Trivial — folds into the `Dialog` close pattern after sheets migrate |

Total: ~270 lines to replace. ~4 hours of work including caller sweeps.

### Tier B — sheets / modals (apply the §3.2 waterfall)

This tier is where the biggest payoff lives (Lesson 6 in CLAUDE.md catalogues the recurring sheet bugs). Applying the iOS-native > RNR > discuss waterfall, the 18 sheets split three ways:

**B.1 — replaced by a native API (file deletes outright)**

| Current sheet | Replacement | Why |
|---|---|---|
| `components/issue/comment-action-sheet.tsx` | `ActionSheetIOS.showActionSheetWithOptions` | One-of-N action menu — exactly what ActionSheetIOS is for. Recommended Phase 3 starter (visible deletion, no styling questions). |
| `components/issue/pickers/due-date-picker-sheet.tsx` | `@react-native-community/datetimepicker` inline picker | Date selection — native API already installed |

**B.2 — replaced by formSheet routes (done)**

The original plan was to swap each picker-sheet for an RNR `Select`. The
mobile-sheet-rollout PR series instead converged on a different shape:
every former picker-sheet now ships as a pure `<XxxPickerBody>` component
under `components/<domain>/pickers/`, embedded inside an Expo Router
formSheet route at `app/(app)/[workspace]/<context>/picker/<field>.tsx`.
This gives the iOS UISheetPresentationController-native chrome
(grabber + detents + spring drag-dismiss) without the per-callsite
state and visibility prop dance an RNR `Select` would still require.

Files in this row are all deleted; their bodies + routes live under the
paths above. No follow-up needed.

**B.3 — genuinely needs a custom-content sheet (RNR `Dialog` pageSheet)**

| Current sheet | Why this one stays as a Dialog |
|---|---|
| `components/issue/issue-filter-sheet.tsx` | Multiple controls in one sheet (filter form), not a list select |
| `components/issue/runs-sheet.tsx` | Run history with rows + actions, not a one-of-N |
| `components/chat/session-sheet.tsx` | TBD — re-inspect when reached; may belong in B.2 |
| `components/chat/agent-picker-sheet.tsx` | Likely B.2 (`Select`) — re-inspect |
| `components/project/add-resource-sheet.tsx` | TBD — depends on whether it's single-select or a mini form |

**B.4 — RNR doesn't ship**

Empty. The only entry — `components/issue/emoji-picker-sheet.tsx` — was
resolved by adopting `rn-emoji-keyboard` and migrating the comment
reaction flow to a formSheet route at
`app/(app)/[workspace]/issue/[id]/comment/[commentId]/emoji-picker.tsx`.
Mobile now ships the full emoji set behind the "More reactions" overflow
in the per-comment actions sheet, matching web parity.

**Rules**:
- Do not bulk-replace `sheet-shell.tsx`. It is imported by 18 files; an atomic swap = 18 simultaneous breakages. Per CLAUDE.md Lesson 6: one PR per sheet, one verification per PR. Sequencing tracked in `~/.claude/plans/mobile-sheet-rollout.md`.
- B.1 sheets are deletions — they should go first, because each one removes a file with zero replacement code (just call `ActionSheetIOS` from the parent). Compound win: less code + matches "defaults first" + matches "iOS native first".
- B.2 sheets simplify the callsite (open a `Select` instead of pushing a sheet), but the imported component IS new code. Do these after B.1.
- B.3 sheets keep their structural complexity; the migration is mostly swapping `Modal` for RNR `Dialog`. Smallest visual change, but the biggest bug fix (drag-dismiss, focus management, safe-area handled by RNR).

### Tier C — domain UI (stays, foundation upgraded)

These are not generic shadcn components — RNR has no equivalent for "priority icon" or "actor avatar". They stay under `components/ui/` for backwards compatibility with current imports, but their **internal building blocks** must move to the RNR foundation: use RNR's `Text` instead of raw `<Text>`, use `cva` for variant tables (not inline conditionals), use semantic tokens (`text-foreground`, never `#1f1f23`).

- `actor-avatar.tsx` (158 lines) — uses `avatar` primitive from RNR underneath, but business logic stays
- `app-header-actions.tsx` (51)
- `avatar-stack.tsx` (97)
- `presence-dot.tsx` (44)
- `priority-icon.tsx` (80)
- `project-icon.tsx` (49)
- `project-priority-icon.tsx` (71)
- `project-status-icon.tsx` (130)
- `status-icon.tsx` (163)
- `pulse-dot.tsx` (52)
- `screen-header.tsx` (37)

No PR for Tier C until a bug or feature touches the file. Then update the foundation as part of that PR — opportunistic, not scheduled.

## 5. Theming architecture

Goal: in-app picker at Settings → Appearance with three options: **Light**, **Dark**, **System**. The selection is persisted in `expo-secure-store` (a key `theme-preference` with values `light` / `dark` / `system`).

### 5.1 Stack

```
global.css                  CSS variables under :root and .dark:root
                              (the source of truth for colors)
tailwind.config.js          darkMode: 'class' + utilities map to hsl(var(--...))
lib/theme.ts                TypeScript mirror of CSS vars, exports NAV_THEME
                              for React Navigation
lib/use-color-scheme.ts     wraps NativeWind's useColorScheme +
                              expo-secure-store persistence
app/_layout.tsx             reads persisted preference at startup,
                              calls setColorScheme(...), wraps Stack in
                              ThemeProvider(NAV_THEME[isDark ? 'dark' : 'light']),
                              mounts <PortalHost /> for RNR dialogs/popovers
```

### 5.2 Token strategy

The current `tailwind.config.js` has hard-coded hex values for ~20 semantic tokens (`background`, `foreground`, `card`, `primary`, …). These become CSS variables under `:root` (light) and `.dark:root` (dark) in `global.css`. Tailwind config switches to `hsl(var(--background))` etc.

Dark mode colors are not in the current config and need to be authored. Two options:

1. **Use shadcn's neutral-base dark palette** (`--background: 0 0% 3.9%`, etc., per RNR's default install) — fastest, gets us a working dark mode immediately, may need a second pass for brand alignment.
2. **Author dark mode by hand** mirroring the web/desktop dark theme — slower, but stays visually consistent with desktop.

Phase 1 starts with **option 1** (shadcn default dark palette) for velocity. A later pass can tune to match desktop's `packages/ui/styles/tokens.css` dark theme once the infrastructure is proven.

Multica-specific tokens not in shadcn's default (`brand`, `brand-foreground`, `success`, `warning`, `info`, `priority`, `code-surface`) get their own CSS variables in both `:root` and `.dark:root`, mapped through `tailwind.config.js` the same way.

### 5.3 Why class-mode, not media-query-mode

Media-query mode (`@media (prefers-color-scheme: dark)`) is the simpler default but cannot be overridden by the app. Multica's Settings → Appearance picker needs to override the OS preference, which requires class mode. The cost of class mode is a single `setColorScheme()` call at app startup to apply the saved preference, paid once before first paint.

### 5.4 `system` option

When the user picks `system`, we call `setColorScheme('system')` (NativeWind v4 supports it natively) and the framework follows the OS via `Appearance.addChangeListener`. We don't need to subscribe ourselves. `isDarkColorScheme` updates reactively.

## 6. Phase plan

### Phase 0 — research & docs *(this document)*

- [x] Read RNR's installation and customization docs verbatim
- [x] Inspect current mobile state (`tailwind.config.js`, `global.css`, `app/_layout.tsx`, `metro.config.js`, `babel.config.js`)
- [x] Update `apps/mobile/CLAUDE.md` with the new UI & theming rules
- [x] Write this migration doc
- [ ] **User verification gate**

### Phase 1 — base infrastructure

Goal: install RNR scaffolding without touching a single existing component. Verification = app builds and runs identically to before; theme switcher in settings works end-to-end.

Checklist:

1. **Add dependencies** (per RNR manual install Step 3):
   - `npx expo install tailwindcss-animate class-variance-authority clsx tailwind-merge @rn-primitives/portal`
   - Confirm `class-variance-authority`, `clsx`, `tailwind-merge`, `@rn-primitives/slot` are already present (they are) — only `tailwindcss-animate` and `@rn-primitives/portal` are new.
2. **Update `metro.config.js`** to set `inlineRem: 16` on `withNativeWind(...)`.
3. **Rewrite `global.css`** with `:root` and `.dark:root` CSS variable blocks (light + dark palettes including Multica's custom tokens).
4. **Rewrite `tailwind.config.js`** to use `hsl(var(--...))` mappings, set `darkMode: 'class'`, register `tailwindcss-animate` plugin, add `hairlineWidth()` border width. Keep mobile-specific overrides (`borderRadius`, custom tokens).
5. **Create `lib/theme.ts`** — TS mirror of CSS variables + `NAV_THEME` for React Navigation.
6. **Create `lib/use-color-scheme.ts`** — wraps NativeWind's `useColorScheme()`, loads/persists preference from `expo-secure-store` on mount, exposes `{ colorScheme, isDarkColorScheme, setColorScheme }`.
7. **Create `components.json`** with the standard RNR configuration (`style: "new-york"`, paths, aliases pointing at `@/components`, `@/lib/utils`).
8. **Update `app/_layout.tsx`**:
   - Read persisted theme preference (sync read from secure-store at module init or first effect; default `system`).
   - Apply it via `setColorScheme(...)` before children render.
   - Wrap `Stack` in `ThemeProvider(NAV_THEME[isDarkColorScheme ? 'dark' : 'light'])` from `@react-navigation/native`.
   - Mount `<PortalHost />` from `@rn-primitives/portal` as the last child of providers.
9. **Settings → Appearance picker**: add an "Appearance" `SectionGroup` to `app/(app)/[workspace]/more/settings.tsx` with three rows (Light / Dark / System), calling `setColorScheme(mode)` + persisting. UI mirrors the existing `WorkspaceRow` pattern; no new dependencies.
10. **Verify**: app builds; all existing screens render unchanged in light mode; toggle Dark → backgrounds invert + text inverts; toggle System → matches the simulator's OS appearance; kill app, reopen → preference persisted.

**Phase 1 does NOT replace any existing component.** Buttons, inputs, sheets are still the hand-written versions. The dark mode "works" against the existing hex-derived semantic tokens because we are simply remapping them through CSS variables — the same `bg-background` class now resolves to a CSS variable that has two values instead of one.

**Risk**: components that style with hard-coded hex (e.g. `<Ionicons color="#71717a">`, `bg-[#fafafa]`) will not respond to theme change. Phase 1 includes a grep sweep for `#[0-9a-fA-F]{3,6}` in `components/` and `app/` — every hit is either (a) replaceable with a token now, or (b) flagged for Phase 3 with a `TODO(rnr-migration):` comment.

### Phase 2 — first component (canary)

Goal: prove the migration mechanics on the simplest non-trivial component before doing 20 of them.

**Pick**: `button.tsx`. Reasons:
- High caller count (`grep -rn 'from "@/components/ui/button"' apps/mobile/`) — exercises the import-sweep pattern.
- RNR ships a button with `variant` and `size` props that mirror shadcn — confirms API parity.
- Visible everywhere — visual regressions are immediately obvious.

Steps:

1. `npx @react-native-reusables/cli@latest add button`. CLI writes `components/ui/button.tsx` over the existing file (the existing one becomes the diff baseline in git).
2. Diff old vs new. Note any prop or visual differences (e.g. RNR's `variant` enum vs ours, default size differences).
3. Sweep every caller. If new RNR API requires different props, adjust callers in the same PR.
4. Visual diff in simulator: open every screen that uses a button, screenshot vs main branch.
5. Verify in both light and dark mode.

Phase 2 is one PR. Acceptance: every existing button still works; no regressions in light mode; sensible defaults in dark mode.

### Phase 3 — everything else

Order:

1. **Other Tier A primitives** (input, text-field, card, text, textarea) — one PR per component or group, same canary pattern as button.
2. **`text` is special**: it's imported by virtually every file. Plan a dedicated PR; the codemod is mechanical (`import { Text } from "@/components/ui/text"` already exists; the file behind it changes).
3. **Tier B sheets** — proceed per `~/.claude/plans/mobile-sheet-rollout.md`, one sheet per PR. **Order: B.1 first (native-API replacements that delete files), then B.2 (`Select` replacements), then B.3 (`Dialog` migrations).** First sheet PR = `comment-action-sheet.tsx` → `ActionSheetIOS` because it's a clean deletion and validates the §3.2 waterfall in practice. Stop and re-verify after each.
4. **Tier C foundation upgrades** — opportunistic, no scheduled PRs.
5. **Cleanup PR**: remove legacy files that are no longer referenced; remove any TODO comments from Phase 1's hex sweep; final `pnpm typecheck && pnpm lint` clean.

Stopping rules for Phase 3:

- If three consecutive PRs introduce visual regressions, **pause** and reconsider token mapping. Don't power through.
- If a Tier B sheet fails to migrate cleanly (RNR `dialog` doesn't fit the use case), document the divergence in this doc and keep the hand-written version, marked as "intentional exception" rather than "to-do".

## 7. Known pitfalls

These are the gotchas that came up during research; encode in your reflexes before doing Phase 1.

1. **`darkMode: 'class'` + `.dark:root`** is the *only* combination that works with NativeWind v4 for class-controlled mode. Do not use the standard `.dark` selector — NativeWind needs the `:root` suffix to apply globally. Source: RNR `customization.mdx`.
2. **`setColorScheme()` is from NativeWind, not React Native.** Importing from `react-native` gives you the read-only OS value. The NativeWind version supports `setColorScheme('light' | 'dark' | 'system')` and triggers re-render. Source: NativeWind 4 docs.
3. **Sync gotcha**: `lib/theme.ts` and `global.css` must mirror each other. If you change one without the other, components styled by Tailwind classes will look right but anything reading `THEME` directly (e.g. inline styles, animations, React Navigation chrome) will be wrong. The RNR docs ship a sync prompt template — use it.
4. **`AbortSignal.timeout` / `AbortSignal.any` still don't exist in Hermes** (Lesson 5). No bearing on RNR migration but worth re-stating since any new component that does its own network calls (autosize-textarea uses no network, button uses none — but in general) needs the manual AbortController pattern.
5. **`useSafeAreaInsets` inside RNR `Dialog`** has the same flakiness as inside raw `Modal`. The pageSheet pitfall from CLAUDE.md Lesson 6 still applies — read insets in the parent, pass `bottomInset` as a prop.
6. **`@rn-primitives/portal` `<PortalHost />` placement matters.** It must be the last child of all providers; if mounted inside a provider that re-renders frequently, dialogs unmount unexpectedly. Place once, in `app/_layout.tsx`.
7. **CLI overwrites files without confirmation.** If `components/ui/button.tsx` exists, `add button` replaces it. This is desirable for the migration but disastrous if run accidentally on a Tier C file. Always check git status after each `add`.
8. **NativeWind 5 is on the horizon**, not adopted. CLAUDE.md baseline pins us to v4. RNR v1 supports both; we stay on the v4 install path.

## 8. Open questions

- **Brand color in dark mode.** Current brand is `#4571e0` (light mode only). Dark mode equivalent needs to be picked — either keep it (high contrast on dark bg) or shift. Defer to Phase 1 design pass.
- **`code-surface` token.** Currently `#e8e8eb` (5% darker than `secondary` in light mode). Dark mode equivalent is "5% lighter than dark `secondary`" — author at Phase 1.
- **Settings page UX.** Current `settings.tsx` is a flat list with `SectionGroup`s. The Appearance picker can be either inline (three rows directly) or a row that opens a picker sheet. Inline is simpler for v1; the sheet variant can come once Tier B sheets are migrated.
- **Should we share dark tokens with desktop later?** Desktop's `packages/ui/styles/tokens.css` uses `oklch` (Tailwind v4); mobile uses `hsl` (Tailwind v3.4). Cross-version sharing is impractical; we accept divergence as intentional (existing baseline). Revisit if mobile upgrades to NativeWind 5 + Tailwind v4.

## 9. Appendix — links

- RNR docs: <https://reactnativereusables.com/docs>
- RNR installation (manual steps): <https://reactnativereusables.com/docs/installation>
- RNR customization: <https://reactnativereusables.com/docs/customization>
- RNR CLI: <https://reactnativereusables.com/docs/cli>
- NativeWind v4 dark mode: <https://www.nativewind.dev/docs/core-concepts/dark-mode>
- NativeWind v4 `useColorScheme`: <https://www.nativewind.dev/docs/api/use-color-scheme>
- shadcn theming reference (CSS variable list): <https://ui.shadcn.com/docs/theming>
- RN-Primitives Portal: <https://rnprimitives.com/portal>
- Mobile sheet rollout plan (existing): `~/.claude/plans/mobile-sheet-rollout.md`
- Mobile baseline rules (durable): `apps/mobile/CLAUDE.md`
