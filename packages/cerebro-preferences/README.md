# @multica/cerebro-preferences

Cerebro per-user editor preference settings — currently the
"Enter sends / new line" toggle and the `useSubmitOnEnter` hook that
reads it.

- `views/components/use-submit-on-enter.{ts,test.ts}` — Enter-key
  semantics hook used by chat / comment editors.
- `views/components/enter-preference-section.{tsx,test.tsx}` — settings
  UI that flips the preference.
