import { BillingReturnPage } from "@multica/views/billing";

/**
 * `/billing/return` — where Stripe Checkout and Billing Portal send the browser.
 *
 * Deliberately a top-level route, not workspace-scoped: a deployment configures
 * one set of return URLs in cloud, so they cannot carry a workspace slug. Cloud
 * appends the workspace it authorized and this page resolves it to the slug
 * before forwarding to Workspace Settings → Billing. `billing` is already a
 * reserved slug, so this cannot shadow a real workspace.
 *
 * Desktop has no route of its own here: Stripe opens in the system browser, so
 * the return always lands on the web app. The desktop Billing tab picks the new
 * state up when its window regains focus.
 */
export default function BillingReturnRoute() {
  return <BillingReturnPage />;
}
