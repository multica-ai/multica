import { BillingTestPage } from "@multica/views/billing";

// Account-level test page for the cloud-billing API surface. Despite
// living under [workspaceSlug] — that's where the dashboard layout
// requires every page to sit — none of the data here is workspace-
// scoped. The slug just keeps the route inside the authenticated
// shell.
export default function BillingRoute() {
  return <BillingTestPage />;
}
