// Single source of truth for the zoom every embedded live demo renders at, so
// the hero board and the value-section boards all shrink by the same factor —
// cards/columns end up the exact same on-screen size across the page. Each demo
// lays out at its natural size, then scales down by DEMO_ZOOM.
export const DEMO_ZOOM = 0.85;
