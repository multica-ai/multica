# Decision: Mobile UI is react-native-reusables, taken at its defaults

Status: implemented

## Problem

`apps/mobile/CLAUDE.md` had named react-native-reusables as mobile's shadcn equivalent since the app was bootstrapped, but RNR was never installed. What grew instead was 21 hand-written components under `components/ui/` built from raw `<View>`, `<Text>`, `<Pressable>`, and `<Modal>`, plus 18 sheet and modal files all copying the same shape — a transparent fading `Modal` with a hand-drawn backdrop. That copied shape had already produced a run of bugs: keyboards squashing content, a `maxHeight` clipping a `FlatList`, and `useSafeAreaInsets` returning zero inside a `Modal`.

There was also no theming infrastructure at all. `tailwind.config.js` held hardcoded hex values, `global.css` had only the three Tailwind directives, and there were no CSS variables, no dark mode, and no way for a user to pick one.

## Decision

Mobile uses RNR — NativeWind 4, RN-Primitives, and CVA, the same stack the app already ran — under two standing rules that outlive the migration itself.

**Defaults first.** Accept RNR's default variant, size, spacing, and palette. Do not add wrapper layers, "improved" defaults, or bespoke variants without a concrete product need. The hand-written legacy exists because someone reached for a slightly improved version of a standard primitive; rebuilding that habit on top of RNR would defeat the point. When the CLI writes a component file, use it as-is and adjust callers if the API differs.

**Native, then RNR, then ask.** For any new interaction, stop at the first hit: an iOS or React Native API that already does it (`Alert.prompt`, `Alert.alert`, `ActionSheetIOS`, the community datetime picker, `expo-image-picker`, `expo-document-picker`, `Share.share`, `expo-haptics`); then an RNR component added through its CLI; otherwise stop and ask rather than hand-rolling. This tier is what makes many of the legacy sheets disappear outright instead of being ported.

Theming is class-based dark mode — `darkMode: "class"` with `.dark:root` — plus CSS variables, with a light/dark/system picker persisted in `expo-secure-store`. `lib/theme.ts` mirrors the CSS variables as pre-resolved strings, because CSS variable syntax does not work in React Native's imperative style objects, and `lib/use-color-scheme.ts` is the single source of truth for the current scheme.

Components are classified in three tiers rather than migrated uniformly. Generic primitives are replaced by their RNR equivalents. Sheets and modals go through the native-first waterfall, which deletes many of them. Domain components stay where they are and take the foundation underneath — RNR's `Text`, semantic tokens, CVA variants — without being redesigned.

The rule that outlives all of this lives in [`apps/mobile/CLAUDE.md`](../../../../apps/mobile/CLAUDE.md): new components come from the native tier or from RNR, never hand-written.

## Alternatives considered

Evaluated on stack fit, ownership model, build cost, and accessibility baseline.

**Tamagui.** Rejected. Its main value is one codebase across web and native, which mobile does not need — it is deliberately independent of web and desktop. In exchange it brings its own compiler and styled API, replacing NativeWind, plus bundler configuration and a learning curve.

**Gluestack UI v2.** Rejected. Strong accessibility through `@react-native-aria`, and it can be made to work alongside NativeWind, but its own styled API is the native path. Swapping styling systems has no payoff once NativeWind is already running.

**NativeBase, React Native Paper, RN UI Lib.** Rejected. Mature, but you are locked to the library's components. For a long-lived installed app that sometimes needs to patch a component immediately, the copy-paste ownership model matters more than the maturity — and it matches how `packages/ui/components/ui/` already works on desktop and web, giving one mental model across the repository.

## Consequences

Components are owned outright: they can be patched without waiting for an upstream release, at the cost of not receiving upstream fixes automatically.

Custom tokens beyond the shadcn neutral palette — `brand`, `success`, `warning`, `info`, `priority`, `code-surface` — copy their light values into dark until a screen using them actually breaks there, rather than being authored ahead of demonstrated need.

Mobile's tokens are HSL under Tailwind 3.4 while desktop and web are OKLCH under Tailwind v4. Sharing them across major versions is impractical, so the divergence is intentional.

Some pitfalls are load-bearing and easy to rediscover the hard way. `darkMode: "class"` requires the `.dark:root` selector, not the standard `.dark`, for NativeWind v4 to apply it globally. `setColorScheme()` must come from NativeWind — React Native's export is the read-only OS value. `lib/theme.ts` and `global.css` must mirror each other, or Tailwind-styled components look right while anything reading `THEME` directly is wrong. `@rn-primitives/portal`'s `<PortalHost />` belongs as the last child of all providers, mounted once, or dialogs unmount unexpectedly. `useSafeAreaInsets` is as unreliable inside an RNR `Dialog` as inside a raw `Modal`, so read insets in the parent and pass them down. And the CLI overwrites an existing file without confirming — check `git status` after each add.

Hardcoded hex values do not respond to a theme change, so any that remain are a dark-mode bug waiting for the screen that exposes it.

NativeWind 5 is not adopted; the baseline stays on v4.
