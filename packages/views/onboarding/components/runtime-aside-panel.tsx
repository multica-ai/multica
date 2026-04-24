/**
 * Shared right-rail aside for Step 3 (runtime).
 *
 * Same content on both paths — desktop (runtime-connect FancyView)
 * and web (platform-fork). Explains what a runtime is and reassures
 * the user they can swap later. Designed to live inside a two-column
 * editorial shell's `<aside>` column.
 */
export function RuntimeAsidePanel() {
  return (
    <div className="flex flex-col gap-6">
      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          What&apos;s a runtime?
        </div>
        <p className="text-[14px] leading-[1.6] text-foreground/80">
          A <strong className="font-medium text-foreground">runtime</strong>{" "}
          is a small background process that runs on your machine. It
          connects your workspace to AI coding tools like Claude Code or
          Codex, and executes the tasks your agents pick up.
        </p>
      </section>

      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Good to know
        </div>
        <div className="flex flex-col gap-4">
          <AsideItem
            glyph="↻"
            title="Swap anytime"
            body="Each agent's runtime is just a setting. Change it whenever you want."
          />
          <AsideItem
            glyph="∞"
            title="Add more later"
            body="You can connect a second runtime on another machine for a team, or a dedicated one per agent."
          />
        </div>
      </section>

      <a
        href="https://multica.ai/docs/daemon-runtimes"
        target="_blank"
        rel="noopener noreferrer"
        className="self-start text-[13px] text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        Learn about runtimes →
      </a>
    </div>
  );
}

function AsideItem({
  glyph,
  title,
  body,
}: {
  glyph: string;
  title: string;
  body: string;
}) {
  return (
    <div className="grid grid-cols-[22px_1fr] gap-3">
      <div
        aria-hidden
        className="flex h-[20px] w-[20px] items-center justify-center text-[14px] text-muted-foreground"
      >
        {glyph}
      </div>
      <div className="flex flex-col">
        <div className="text-[13.5px] font-medium text-foreground">{title}</div>
        <div className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
          {body}
        </div>
      </div>
    </div>
  );
}
