import { cn } from "@multica/ui/lib/utils"

const BAR_COUNT = 8
const DURATION = 1.2

const bars = Array.from({ length: BAR_COUNT }, (_, i) => ({
  rotate: `${i * 45}deg`,
  delay: `${-DURATION + (i * DURATION) / BAR_COUNT}s`,
}))

/**
 * Loading — Apple-style radiating-line spinner for **passive waiting** states.
 *
 * Use when the user is waiting for content to arrive (page init, data fetching).
 * For active processing / execution states, use `<Spinner />` instead.
 *
 * Inherits color from `currentColor` (use Tailwind `text-*`).
 * Scales with font-size (use Tailwind `text-*` for size).
 */
function Loading({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      className={cn("text-muted-foreground", className)}
      role="status"
      aria-label="Loading"
      style={{ display: "inline-block", position: "relative", width: "1em", height: "1em" }}
      {...props}
    >
      {bars.map((bar, i) => (
        <span
          key={i}
          style={{
            position: "absolute",
            left: "calc(50% - 0.04em)",
            top: "0.1em",
            width: "0.08em",
            height: "0.24em",
            borderRadius: "1em",
            backgroundColor: "currentColor",
            transformOrigin: "50% 0.4em",
            transform: `rotate(${bar.rotate})`,
            animation: `loading-fade ${DURATION}s linear infinite`,
            animationDelay: bar.delay,
          }}
        />
      ))}

      {/* keyframes injected once via <style> — React deduplicates identical tags */}
      <style>{`@keyframes loading-fade{0%{opacity:1}100%{opacity:.15}}`}</style>
    </span>
  )
}

export { Loading }
