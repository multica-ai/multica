"use client";

// JEH-1067 (Bundle B / PR-B) — Overrides placeholder.
//
// Bundle B owes Member detail page a slot for user-level overrides; the
// full functionality (datamodel + grant/deny mutations + resolver) lands
// in Bundle C. This component renders the empty-state copy so the slot is
// visibly reserved and the page layout is final once Bundle C ships.

export function OverridesPlaceholder() {
  return (
    <section
      className="rounded-md border border-dashed border-border p-4"
      data-testid="overrides-placeholder"
    >
      <h2 className="text-sm font-medium">Overrides</h2>
      <p className="mt-1 text-xs text-muted-foreground">
        Bundle C tilføjer overrides her.
      </p>
    </section>
  );
}
