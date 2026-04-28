"use client";

export function AppearanceTab() {
  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Theme</h2>
        <p className="text-sm text-muted-foreground">
          Multica is currently locked to the Light theme. Dark mode will return
          in a future release.
        </p>
      </section>
    </div>
  );
}
