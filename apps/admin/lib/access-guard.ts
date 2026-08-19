// Kill switch for accidental production exposure. This app has zero
// authentication (explicit "no auth this pass" decision — see the plan's
// "No auth" section) and gives every request direct read access to
// Postgres + LiteLLM via its route handlers. NODE_ENV=production is the
// signal every real deploy path sets (`next start` after `next build`), so
// by default a production run refuses to serve any request rather than
// silently going live unauthenticated. Set ADMIN_ALLOW_UNSAFE_NO_AUTH=true
// to explicitly opt in once the deployment is otherwise access-controlled
// (e.g. sits behind a VPN/IAP) or real auth has since been added here.
//
// Kept as a plain function (no Next.js imports) so it's testable without
// constructing a NextRequest — middleware.ts is the thin framework wrapper.
export function isUnauthenticatedExposureBlocked(env: {
  NODE_ENV?: string;
  ADMIN_ALLOW_UNSAFE_NO_AUTH?: string;
}): boolean {
  return env.NODE_ENV === "production" && env.ADMIN_ALLOW_UNSAFE_NO_AUTH !== "true";
}
