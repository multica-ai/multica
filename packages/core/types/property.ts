/**
 * Custom issue properties — workspace-defined, typed fields on issues
 * (MUL-4463). Definitions live in a workspace catalog (managed by owner/admin
 * only); values live on each issue in a bag keyed by definition id, so
 * renames never touch issue rows.
 *
 * Values are typed per definition: select stores an option id, multi_select
 * an array of option ids (config order), date a "YYYY-MM-DD" string, checkbox
 * a boolean, number a number, text/url strings.
 */
export type IssuePropertyType =
  | "text"
  | "number"
  | "select"
  | "multi_select"
  | "date"
  | "checkbox"
  | "url";

export const ISSUE_PROPERTY_TYPES: IssuePropertyType[] = [
  "text",
  "number",
  "select",
  "multi_select",
  "date",
  "checkbox",
  "url",
];

export function isKnownPropertyType(type: string): type is IssuePropertyType {
  return (ISSUE_PROPERTY_TYPES as string[]).includes(type);
}

export interface IssuePropertyOption {
  id: string;
  name: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
}

export interface IssuePropertyConfig {
  options?: IssuePropertyOption[];
}

export interface IssueProperty {
  id: string;
  workspace_id: string;
  name: string;
  /** Lenient string: newer servers may ship types this client doesn't know. */
  type: string;
  description?: string;
  /** Optional catalog icon key; absent on backends predating icon support. */
  icon?: string;
  config: IssuePropertyConfig;
  position: number;
  archived: boolean;
  archived_at?: string | null;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export type IssuePropertyValue = string | number | boolean | string[];
export type IssuePropertyValues = Record<string, IssuePropertyValue>;

export interface CreatePropertyRequest {
  name: string;
  type: IssuePropertyType;
  description?: string;
  icon?: string;
  config?: IssuePropertyConfig;
}

export interface UpdatePropertyRequest {
  name?: string;
  description?: string;
  /** Empty string clears the icon. */
  icon?: string;
  config?: IssuePropertyConfig;
  archived?: boolean;
}

export interface ListPropertiesResponse {
  properties: IssueProperty[];
  total: number;
}

/** Response of PUT/DELETE /api/issues/{id}/properties/{propertyId}: the full post-mutation bag. */
export interface IssuePropertiesResponse {
  properties: IssuePropertyValues;
}
