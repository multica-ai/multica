// Shared construction for Multica glyphs: a 24px grid, continuous-curvature
// (squircle) corners and soft stroke ends. Drawings are inner SVG markup;
// `class="b"` marks the body silhouette and `class="k"` a detail cut into it.

export const f = (n) => +n.toFixed(2);
const dist = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1]);
const unit = (a, b) => {
  const d = dist(a, b) || 1;
  return [(a[0] - b[0]) / d, (a[1] - b[1]) / d];
};
const pt = (p) => `${f(p[0])} ${f(p[1])}`;

function corner(prev, v, next, k) {
  const ua = unit(prev, v);
  const ub = unit(next, v);
  const kk = Math.min(k, dist(prev, v) / 2, dist(next, v) / 2);
  const p = (u, t) => [v[0] + u[0] * t, v[1] + u[1] * t];
  const m = [v[0] + (ua[0] + ub[0]) * 0.146 * kk, v[1] + (ua[1] + ub[1]) * 0.146 * kk];
  return {
    start: p(ua, kk),
    curve: `C${pt(p(ua, 0.5 * kk))} ${pt(p(ua, 0.293 * kk))} ${pt(m)} C${pt(p(ub, 0.293 * kk))} ${pt(p(ub, 0.5 * kk))} ${pt(p(ub, kk))}`,
  };
}

/**
 * Path through `[x, y, k]` points, rounding each vertex with a squircle corner
 * of size `k`. Closed shapes round every vertex; open paths keep their ends.
 */
export function shape(pts, closed = true) {
  const n = pts.length;
  const at = (i) => pts[(i + n) % n];
  let d = closed ? "" : `M${pt(pts[0])}`;
  const first = closed ? 0 : 1;
  const last = closed ? n - 1 : n - 2;
  for (let i = first; i <= last; i++) {
    const c = corner(at(i - 1), at(i), at(i + 1), at(i)[2] ?? 0);
    d += (d ? ` L${pt(c.start)} ` : `M${pt(c.start)} `) + c.curve;
  }
  return closed ? `${d} Z` : `${d} L${pt(pts[n - 1])}`;
}

export const rect = (x, y, w, h, k) => shape([[x, y, k], [x + w, y, k], [x + w, y + h, k], [x, y + h, k]]);

/** Regular polygon or star: `radii` alternate around the centre, starting at `start` degrees. */
export function polygon(cx, cy, radii, count, k, start = -90) {
  const pts = [];
  for (let i = 0; i < count * radii.length; i++) {
    const r = radii[i % radii.length];
    const a = ((start + (i * 360) / (count * radii.length)) * Math.PI) / 180;
    pts.push([cx + r * Math.cos(a), cy + r * Math.sin(a), Array.isArray(k) ? k[i % k.length] : k]);
  }
  return shape(pts);
}

export function gear() {
  const pts = [];
  for (let i = 0; i < 8; i++) {
    const a = (i / 8) * Math.PI * 2 - Math.PI / 2;
    const P = (r, t) => [12 + r * Math.cos(t), 12 + r * Math.sin(t), 1.1];
    pts.push(P(7.4, a - 0.3), P(9.6, a - 0.17), P(9.6, a + 0.17), P(7.4, a + 0.3));
  }
  return shape(pts);
}

/**
 * Arrowhead with a sharp tip at (x, y), pointing `dir` degrees
 * (0 right, 90 down, 180 left, 270 up), `depth` long and `half` wide each side.
 */
export function head(x, y, dir, depth, half) {
  const a = (dir * Math.PI) / 180;
  const c = Math.cos(a);
  const s = Math.sin(a);
  const P = (u, v) => pt([x + u * c - v * s, y + u * s + v * c]);
  return `<path d="M${P(-depth, -half)}L${P(0, 0)} ${P(-depth, half)}"/>`;
}

export const S = 'fill="currentColor" stroke="none"';
export const dot = (x, y, r = 1.15) => `<circle ${S} cx="${x}" cy="${y}" r="${r}"/>`;
