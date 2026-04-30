import { Instrument_Serif } from "next/font/google";

// Editorial-style 404. Cream + ink + terracotta palette is intentionally
// inline — these brand experiments have not been promoted to design tokens.
// The route lives outside the (landing) group's font scope, so we attach
// Instrument Serif locally to match the editorial direction.
const CREAM = "#faf9f6";
const INK = "#1b1812";
const TERRACOTTA = "#a64a2c";

const editorialSerif = Instrument_Serif({
  subsets: ["latin"],
  weight: "400",
  variable: "--font-serif",
});

export default function NotFound() {
  return (
    <section
      className={`${editorialSerif.variable} relative flex min-h-screen flex-col items-center justify-center px-6 py-16`}
      style={{ backgroundColor: CREAM, color: INK }}
    >
      {/* tracking is wider than Tailwind's tracking-widest (0.1em) — editorial eyebrow detail, deliberate. */}
      <div
        className="flex items-center gap-3 text-xs uppercase tracking-[0.25em]"
        style={{ color: TERRACOTTA }}
      >
        <span aria-hidden="true" className="inline-block h-px w-10" style={{ background: TERRACOTTA }} />
        <span>error · not found</span>
        <span aria-hidden="true" className="inline-block h-px w-10" style={{ background: TERRACOTTA }} />
      </div>

      {/* Fluid hero size + ultra-tight leading; outside the Tailwind type scale by design. */}
      <h1 className="mt-12 font-serif text-[clamp(7rem,16vw,15rem)] leading-[0.85] tracking-tight">
        404
      </h1>

      <p className="mt-10 max-w-xl text-center font-serif text-3xl leading-tight">
        This page{" "}
        <em className="not-italic" style={{ color: TERRACOTTA }}>
          doesn&rsquo;t exist
        </em>
        .
      </p>
      <p
        className="mt-5 max-w-md text-center text-sm leading-relaxed"
        style={{ color: INK, opacity: 0.6 }}
      >
        The URL may have changed, the resource may be deleted, or you arrived from a stale link.
      </p>

      <a
        href="/"
        className="mt-12 inline-flex h-10 items-center rounded-full px-6 text-sm font-medium transition hover:opacity-90"
        style={{ background: INK, color: CREAM }}
      >
        Back to Multica
      </a>
    </section>
  );
}
