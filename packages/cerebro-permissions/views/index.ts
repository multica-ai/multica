// FIR-2230 phase 7: the old Persona "Permissions" page (PermissionsPage) and
// its nav item were folded into the single "Access" page — its Matrix, Roles,
// and Effective tabs now live alongside People & Agents / Permissions / Audit
// on AccessPage. One access surface, not two.
export { AccessPage } from "./access-page";
export { AccessNavItem } from "./components/access-nav-item";
// FIR-2230 table now lives in the viewless @multica/cerebro-tool-policy leaf
// package (re-exported here for existing consumers of cerebro-permissions/views).
export { ToolPolicyTable, type ToolPolicyTableProps } from "@multica/cerebro-tool-policy/views";
