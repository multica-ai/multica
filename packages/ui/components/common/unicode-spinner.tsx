"use client";

import { useEffect, useState } from "react";
import spinners, { type BrailleSpinnerName } from "unicode-animations";

interface Props {
  name?: BrailleSpinnerName;
  className?: string;
  /** Stop advancing frames without unmounting (e.g., when an outer state freezes). */
  paused?: boolean;
}

// Inline-rendered braille spinner. Each frame is a unicode string from the
// `unicode-animations` package; we tick frames on the spinner's own `interval`
// and render the current one inside a fixed-width monospace span so different
// frames never reflow neighbouring text. Width-jitter is the main reason this
// component exists rather than dropping the raw strings into Tailwind classes.
export function UnicodeSpinner({ name = "braille", className, paused }: Props) {
  const spec = spinners[name];
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    if (paused) return;
    setFrame(0);
    const timer = setInterval(
      () => setFrame((f) => (f + 1) % spec.frames.length),
      spec.interval,
    );
    return () => clearInterval(timer);
  }, [name, paused, spec]);

  return (
    <span
      aria-hidden="true"
      className={className}
      style={{
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
        display: "inline-block",
        minWidth: "1ch",
        textAlign: "center",
        fontVariantNumeric: "tabular-nums",
      }}
    >
      {spec.frames[frame]}
    </span>
  );
}
