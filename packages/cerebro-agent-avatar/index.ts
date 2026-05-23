// Cerebro-only extensions for the agent avatar surface — AI generation on
// top of the upstream manual-upload AvatarPicker. Imported from upstream
// zone via thin marker blocks; see docs/cerebro-patches.md
// (`agent-avatar-generate`).

// Side-effect import: pulls the i18n namespace augmentation into the
// compilation graph so `useT("cerebro-agent-avatar")` resolves to the
// shape in `locales/en.json` for downstream consumers.
import "./i18n-types";

export { CerebroAvatarPicker } from "./avatar-picker";
export { CerebroInspectorAvatar } from "./inspector-avatar";
