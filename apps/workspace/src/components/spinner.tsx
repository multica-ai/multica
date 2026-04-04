/**
 * Spinner — 3x3 grid pulse for **active processing / execution** states.
 *
 * Use when the system is actively doing work or waiting for human action
 * (streaming content, generating responses, awaiting approval).
 * For passive content-loading states, use `<Loading />` instead.
 *
 * Inherits color from `currentColor` (use Tailwind `text-*`).
 * Scales with font-size (use Tailwind `text-*` for size).
 */
import { cn } from "@/lib/utils"

export interface SpinnerProps {
  /** Additional className for styling (color via text-*, size via Tailwind text-*) */
  className?: string
}

const DELAYS = [0.2, 0.3, 0.4, 0.1, 0.2, 0.3, 0, 0.1, 0.2]

const cubeStyle: React.CSSProperties = {
  backgroundColor: "currentColor",
  animation: "spinner-grid 1.3s infinite ease-in-out",
  transform: "scale3d(0.5, 0.5, 1)",
}

export function Spinner({ className }: SpinnerProps) {
  return (
    <span
      className={cn(className)}
      role="status"
      aria-label="Loading"
      style={{
        display: "inline-grid",
        gridTemplateColumns: "repeat(3, 1fr)",
        width: "1em",
        height: "1em",
        gap: "0.08em",
      }}
    >
      {DELAYS.map((delay, i) => (
        <span key={i} style={{ ...cubeStyle, animationDelay: `${delay}s` }} />
      ))}

      <style>{`@keyframes spinner-grid{0%,70%,100%{transform:scale3d(.5,.5,1)}35%{transform:scale3d(0,0,1)}}`}</style>
    </span>
  )
}
