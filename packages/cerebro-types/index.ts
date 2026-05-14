export type CerebroNamespace = "cerebro";

export type {
  Account,
  CreateAccountRequest,
  UpdateAccountControlsRequest,
} from "./account";

// Side-effect import — runs the module-augmentation declarations in
// augment.ts. Any package that wants the augmented upstream interfaces in
// scope should depend on @multica/cerebro-types; importing this index pulls
// the augmentation into the typecheck.
import "./augment";

// JEH-1290 W8 — agent tool grant types (cerebro-only, not in upstream)
export type { AgentTool } from "./augment";
