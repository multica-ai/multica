export type StrategyKind = "core_value" | "core_focus" | "horizon_goal" | "unknown";
export type StrategyState = "active" | "archived" | "unknown";
export type HealthState = "on_track" | "at_risk" | "off_track" | "unset" | "unknown";
export type HorizonUnit = "day" | "week" | "month" | "year";

export interface Terminology { strategy: string; rock: string; rocks: string }
export interface OperatingSystemSettings { workspace_id: string; terminology: Terminology; created_at?: string; updated_at?: string }
export interface StrategyItem { id: string; workspace_id: string; kind: StrategyKind; title: string; description: string; horizon_unit?: HorizonUnit; horizon_count?: number; position: number; state: StrategyState; created_at: string; updated_at: string }
export interface StrategyItemInput { kind: Exclude<StrategyKind, "unknown">; title: string; description?: string; horizon_unit?: HorizonUnit; horizon_count?: number; position: number; state?: Exclude<StrategyState, "unknown"> }
export interface DerivedHealth { state: HealthState; reason: string; calculated_at: string }
export interface Rock { project_id: string; workspace_id: string; project_title: string; project_description?: string; project_status: string; lead_type?: string; lead_id?: string; period_start: string; period_end: string; confidence: number; reported_health: HealthState; derived_health: DerivedHealth; issue_count: number; done_issue_count: number; blocked_issue_count: number; created_at: string; updated_at: string }
export interface RockInput { project_id: string; period_start: string; period_end: string; confidence: number; reported_health: Exclude<HealthState, "unknown"> }
export interface ObjectConnectionInput { source_type: string; source_id: string; target_type: string; target_id: string; relationship_type?: string; provenance?: "manual" | "agent" | "system" }
export interface ObjectConnection extends Required<ObjectConnectionInput> { id: string; workspace_id: string; created_by_type: string; created_by_id: string; created_at: string }
export interface ObjectConnectionList { connections: ObjectConnection[] }
export interface StrategyList { strategy_items: StrategyItem[] }
export interface RocksList { rocks: Rock[] }
