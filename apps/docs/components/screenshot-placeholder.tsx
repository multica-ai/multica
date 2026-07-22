export function ScreenshotPlaceholder({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="not-prose my-7 flex min-h-[240px] items-center justify-center rounded-lg border border-dashed border-[var(--docs-rule)] bg-muted/20 px-6 text-center">
      <div className="max-w-lg">
        <div className="mb-2 font-mono text-[0.6875rem] font-semibold uppercase tracking-[0.1em] text-[var(--primary)]">
          {title}
        </div>
        <p className="m-0 text-sm leading-6 text-muted-foreground">
          {description}
        </p>
      </div>
    </div>
  );
}
