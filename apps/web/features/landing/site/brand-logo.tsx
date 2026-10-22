import { cn } from "@multica/ui/lib/utils";

/*
 * Multica lockup, brand guidelines v2.0. Geometry in grid units (u = 10):
 * a 5×5 dot matrix with 0.9u dots, 13 lit dots spelling M, the centre dot
 * in agent amber, the other 12 at 8% of the ink. The outlined Inter Display
 * SemiBold wordmark has the same 4.9u ink height and sits 2u to the right.
 * Do not retype the wordmark or change the proportions.
 */
const U = 10;
const R = 4.5;
const MARK_INK = 49;
const WORD_X = MARK_INK + 2 * U;
const WORD = { w: 3064.1, h: 744.1 };
const WORD_SCALE = MARK_INK / WORD.h;
const WIDTH = +(WORD_X + WORD.w * WORD_SCALE).toFixed(2);

const LIT = new Set(["0,0", "0,4", "1,0", "1,1", "1,3", "1,4", "2,0", "2,2", "2,4", "3,0", "3,4", "4,0", "4,4"]);
const DOTS = Array.from({ length: 25 }, (_, i) => {
  const r = Math.floor(i / 5);
  const c = i % 5;
  const key = `${r},${c}`;
  return { key, cx: R + c * U, cy: R + r * U, lit: LIT.has(key), agent: key === "2,2" };
});

const WORDMARK_PATH =
  "M0 732.4H123V424.8C123 351.1 170.4 311 226.1 311C280.8 311 319.8 347.7 319.8 403.8V732.4H439.5V416C439.5 354.5 478 311 540.5 311C593.3 311 636.2 341.3 636.2 409.7V732.4H759.8V392.1C759.8 271 684.6 206.5 586.9 206.5C511.7 206.5 450.2 244.6 422.9 304.2C401.4 245.1 344.2 206.5 274.4 206.5C209.5 206.5 150.4 239.7 119.1 306.2V216.8H0ZM1022.2 742.7C1093.5 742.7 1145.8 711.4 1180.9 653.3V732.4H1302V216.8H1178.5V508.3C1178.5 596.2 1130.6 634.8 1065.7 634.8C999.8 634.8 961.7 595.2 961.7 519.5V216.8H838.7V539.1C838.7 671.4 914.3 742.7 1022.2 742.7ZM1504.5 4.9H1381.4V732.4H1504.5ZM1860.7 216.8H1755.7V76.2H1632.2V216.8H1542.3V315.9H1632.2V602.5C1632.2 690.9 1681.5 732.4 1786.5 732.4H1860.7V633.3H1807.9C1767.9 633.3 1755.7 621.1 1755.7 584.5V315.9H1860.7ZM1908.3 732.4H2031.4V216.8H1908.3ZM1969.8 140.1C2011.8 140.1 2043.6 109.9 2043.6 69.8C2043.6 30.3 2011.8 0 1969.8 0C1927.9 0 1896.1 30.3 1896.1 69.8C1896.1 109.9 1927.9 140.1 1969.8 140.1ZM2338.8 744.1C2469.1 744.1 2563.9 660.2 2572.7 547.4H2451.1C2439.4 604.5 2404.7 641.6 2340.2 641.6C2259.2 641.6 2211.3 576.7 2211.3 475.1C2211.3 373 2259.7 307.6 2340.2 307.6C2404.2 307.6 2442.8 346.2 2452.1 401.4H2572.7C2561.9 286.6 2468.2 205.1 2338.8 205.1C2187.9 205.1 2086.8 315.9 2086.8 475.1C2086.8 634.3 2187.9 744.1 2338.8 744.1ZM2789.2 740.7C2873.2 740.7 2916.7 705.6 2941.1 659.7H2943V732.4H3064.1V380.9C3064.1 273.4 2984.6 206.5 2850.3 206.5C2715.5 206.5 2631.5 274.4 2625.7 377H2744.3C2747.7 333.5 2787.8 302.2 2847.8 302.2C2906.9 302.2 2942.1 333.5 2942.1 377.4V381.3C2942.1 416.5 2909.4 418 2813.7 428.7C2707.2 439.9 2611 468.8 2611 585.9C2611 689 2686.7 740.7 2789.2 740.7ZM2819.5 649.4C2765.8 649.4 2731.1 625.5 2731.1 585C2731.1 538.1 2776.1 519 2828.8 511.2C2879.6 503.4 2927.4 495.6 2942.6 485.8V541.5C2942.6 601.6 2900.1 649.4 2819.5 649.4Z";

/** Horizontal lockup; `height` is the shared ink height of mark and wordmark (12px minimum on screen). */
export function BrandLogo({ height = 18, className }: { height?: number; className?: string }) {
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${MARK_INK}`}
      height={height}
      width={(height * WIDTH) / MARK_INK}
      aria-hidden
      className={cn("shrink-0 text-site-ink", className)}
    >
      {DOTS.map((dot) => (
        <circle
          key={dot.key}
          cx={dot.cx}
          cy={dot.cy}
          r={R}
          className={dot.agent ? "fill-site-agent" : "fill-current"}
          fillOpacity={dot.lit ? undefined : 0.08}
        />
      ))}
      <path
        transform={`translate(${WORD_X} 0) scale(${WORD_SCALE.toFixed(6)})`}
        className="fill-current"
        d={WORDMARK_PATH}
      />
    </svg>
  );
}
