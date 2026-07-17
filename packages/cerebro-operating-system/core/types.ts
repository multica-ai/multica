export type StrategyKind = "core_value" | "core_focus" | "horizon_goal" | "unknown";
export type StrategyState = "active" | "archived" | "unknown";
export type HealthState = "on_track" | "at_risk" | "off_track" | "unset" | "unknown";
export type HorizonUnit = "day" | "week" | "month" | "year";

export interface Terminology {
  strategy: string; rock: string; rocks: string;
  vision_plan: string; meetings: string; org_chart: string;
  scorecard: string; issues_list: string; strategy_map: string;
}
export type OsElementKey = "vision_plan" | "goals" | "meetings" | "org_chart" | "scorecard" | "issues_list" | "strategy_map";
export interface OsElement { key: string; enabled: boolean; default_enabled: boolean }
export interface OsElementList { elements: OsElement[] }
export type PeriodUnit = "month" | "quarter" | "custom";
export interface GoalType { id: string; workspace_id: string; name: string; color: string; scope_label: string; position: number; created_at: string; updated_at: string }
export interface GoalTypeList { goal_types: GoalType[] }
export interface GoalTypeInput { name: string; color?: string; scope_label?: string; position?: number }
export interface OperatingPeriodInput { name?: string; unit: PeriodUnit; starts_on: string; ends_on?: string }
export interface OperatingSystemSettings { workspace_id: string; terminology: Terminology; created_at?: string; updated_at?: string }
export interface OperatingPeriod { id: string; workspace_id: string; name: string; unit: PeriodUnit; starts_on: string; ends_on: string }
export interface OperatingPeriodList { periods: OperatingPeriod[] }
export interface StrategyItem { id: string; workspace_id: string; kind: StrategyKind; title: string; description: string; horizon_unit?: HorizonUnit; horizon_count?: number; horizon_label?: string; position: number; state: StrategyState; created_at: string; updated_at: string }
export interface StrategyItemInput { kind: Exclude<StrategyKind, "unknown">; title: string; description?: string; horizon_unit?: HorizonUnit; horizon_count?: number; horizon_label?: string; position: number; state?: Exclude<StrategyState, "unknown"> }
export interface DerivedHealth { state: HealthState; reason: string; calculated_at: string }
export interface RockProject { id: string; title: string; issue_count: number; done_issue_count: number }
export interface RockIssue { id: string; identifier: string; title: string; status: string; project_id?: string; project_title?: string }
export interface RockCheckIn { id: string; confidence: number; reported_health: HealthState; note: string; created_by_type: string; created_by_id: string; created_at: string }
export interface Rock {
  id: string; workspace_id: string; title: string; description?: string;
  owner_type?: string; owner_id?: string; owner_name?: string;
  period_id: string; period_name: string; period_start: string; period_end: string;
  goal_type_id?: string; goal_type_name?: string; goal_type_color?: string; goal_type_scope_label?: string;
  confidence: number; reported_health: HealthState; derived_health: DerivedHealth; health_score: number;
  issue_count: number; done_issue_count: number; blocked_issue_count: number; project_count: number;
  projects: RockProject[]; issues: RockIssue[]; check_ins: RockCheckIn[];
  strategy_item_id?: string; strategy_item_title?: string;
  project_id?: string; project_title?: string; project_description?: string; project_status?: string;
  lead_type?: string; lead_id?: string; created_at: string; updated_at: string;
}
export interface RockInput {
  title: string; description?: string; owner_type?: "member" | "agent"; owner_id?: string;
  period_id: string; goal_type_id?: string; confidence: number; reported_health: Exclude<HealthState, "unknown">;
  project_ids: string[]; issue_ids: string[]; strategy_item_id?: string;
}
export interface RockCheckInInput { confidence: number; reported_health: Exclude<HealthState, "unknown">; note?: string }
export interface ObjectConnectionInput { source_type: string; source_id: string; target_type: string; target_id: string; relationship_type?: string; provenance?: "manual" | "agent" | "system" }
export interface ObjectConnection extends Required<ObjectConnectionInput> { id: string; workspace_id: string; created_by_type: string; created_by_id: string; created_at: string }
export interface ObjectConnectionList { connections: ObjectConnection[] }
export interface StrategyList { strategy_items: StrategyItem[] }
export interface StrategyHistoryEntry { id: string; strategy_item_id: string; action: "baseline" | "created" | "updated" | "deleted"; title: string; snapshot: Record<string, unknown>; changed_at: string }
export interface StrategyHistoryList { history: StrategyHistoryEntry[] }
export interface RocksList { rocks: Rock[] }
