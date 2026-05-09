// Ship Hub view package — public surface. Both apps import from here and
// never reach into ./components/* directly so this file is the contract.
export { ShipPage } from "./components/ship-page";
export { ShipKanban } from "./components/ship-kanban";
export { ShipPRCard } from "./components/ship-pr-card";
export { ShipDeployStrip } from "./components/ship-deploy-strip";
export { ShipProjectSection } from "./components/ship-project-section";
export { ShipEmptyState } from "./components/ship-empty-state";
export { ShipNoTokenState } from "./components/ship-no-token-state";
export { ConfigureDeployEnvDialog } from "./components/configure-deploy-env-dialog";
export { LogDeployDialog } from "./components/log-deploy-dialog";

export {
  bucketPullRequests,
  deriveShipKanbanColumn,
  isFailingOrBlocked,
  deriveRiskHint,
} from "./hooks/use-pr-state";
export type { ShipKanbanColumn, KanbanBuckets } from "./hooks/use-pr-state";
