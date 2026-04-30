// Soft warn at 90 % of the cap, hard error past it. Shared between the
// description editor (modal) and the create-agent dialog so both surfaces
// read the same way. Renders a single inline line so it can sit under any
// textarea / input without disturbing surrounding spacing.
export function CharCounter({ length, max }: { length: number; max: number }) {
  const over = length > max;
  const near = !over && length >= Math.floor(max * 0.9);
  const tone = over
    ? "text-destructive"
    : near
      ? "text-warning"
      : "text-muted-foreground";
  return (
    <div className={`text-right text-xs tabular-nums ${tone}`}>
      {length} / {max}
      {over && ` · ${length - max} over limit`}
    </div>
  );
}
