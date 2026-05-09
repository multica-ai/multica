// Ship Hub view package — public surface. Both apps import from here and
// never reach into ./components/* directly so this file is the contract.
export { ShipPage } from "./components/ship-page";
export { ShipKanban } from "./components/ship-kanban";
export { ShipPRCard } from "./components/ship-pr-card";
// Phase 2 replaces the lightweight strip with animated swimlanes; the old
// component is removed since the product isn't live yet (per CLAUDE.md
// "no compatibility shims for non-boundary code").
export { ShipDeploySwimlanes } from "./components/ship-deploy-swimlanes";
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
  EMPTY_DEPLOY_SNAPSHOT,
} from "./hooks/use-pr-state";
export type {
  ShipKanbanColumn,
  KanbanBuckets,
  ShipDeploySnapshot,
} from "./hooks/use-pr-state";

// Phase 3 — chip derivation + components
export { derivePrChips } from "./hooks/use-pr-chips";
export type {
  PrChip,
  PrChipVariant,
  PrChipInputs,
} from "./hooks/use-pr-chips";
export { PrChipRow } from "./components/pr-chip-row";
export { ChipButton } from "./components/chip-button";
// Phase 6.5 — submit-review dialog
export { ReviewDialog } from "./components/review-dialog";

// Phase 7a — Releases
export { ShipReleasePage } from "./components/ship-release-page";
export { ShipSelectionBar } from "./components/ship-selection-bar";
export { CreateReleaseDialog } from "./components/create-release-dialog";
export { ShipActiveReleasesRail } from "./components/ship-active-releases-rail";
