import type { ProjectResource } from "./project";

export type CRMAccountStatus = "active" | "inactive" | "prospect" | "archived";
export type CRMAccountType = "prospect" | "customer" | "partner" | "supplier" | "competitor" | "other";
export type CRMAccountSource = "manual" | "email" | "whatsapp" | "website" | "referral" | "trade_show" | "linkedin" | "other";
export type CRMAccountRating = "hot" | "warm" | "cold" | "unknown";
export type CRMAccountPriority = "high" | "medium" | "low";

export interface CRMAccount {
  id: string;
  workspace_id: string;
  name: string;
  account_code?: string | null;
  account_type: CRMAccountType;
  website?: string | null;
  country?: string | null;
  country_code?: string | null;
  country_name?: string | null;
  region?: string | null;
  city?: string | null;
  industry?: string | null;
  sub_industry?: string | null;
  status: CRMAccountStatus;
  owner_id?: string | null;
  owner_member_id?: string | null;
  source?: CRMAccountSource | null;
  rating: CRMAccountRating;
  priority: CRMAccountPriority;
  annual_revenue?: string | null;
  employee_count?: string | null;
  tags: string[];
  notes?: string | null;
  last_contacted_at?: string | null;
  next_follow_up_at?: string | null;
  contact_count: number;
  created_at: string;
  updated_at: string;
}

export type CRMAccountFollowUpBucket = "today" | "next_7_days" | "overdue" | "none";
export type CRMAccountSort = "updated" | "name" | "next_follow_up" | "priority_rating";

export interface ListCRMAccountsParams {
  search?: string;
  status?: CRMAccountStatus | "";
  rating?: CRMAccountRating | "";
  priority?: CRMAccountPriority | "";
  country_code?: string;
  industry?: string;
  source?: CRMAccountSource | "";
  follow_up_bucket?: CRMAccountFollowUpBucket | "";
  sort?: CRMAccountSort;
}

export interface ListCRMAccountsResponse {
  accounts: CRMAccount[];
  total: number;
}

export interface CreateCRMAccountRequest {
  name: string;
  account_code?: string | null;
  account_type?: CRMAccountType;
  website?: string | null;
  country?: string | null;
  country_code?: string | null;
  country_name?: string | null;
  region?: string | null;
  city?: string | null;
  industry?: string | null;
  sub_industry?: string | null;
  status?: CRMAccountStatus;
  owner_id?: string | null;
  owner_member_id?: string | null;
  source?: CRMAccountSource | null;
  rating?: CRMAccountRating;
  priority?: CRMAccountPriority;
  annual_revenue?: string | null;
  employee_count?: string | null;
  tags?: string[];
  notes?: string | null;
  last_contacted_at?: string | null;
  next_follow_up_at?: string | null;
}

export type UpdateCRMAccountRequest = CreateCRMAccountRequest;

export type CRMContactDecisionRole = "decision_maker" | "influencer" | "buyer" | "user" | "finance" | "technical" | "gatekeeper" | "other";

export interface CRMContact {
  id: string;
  workspace_id: string;
  account_id?: string | null;
  name: string;
  salutation?: string | null;
  email?: string | null;
  phone?: string | null;
  mobile?: string | null;
  whatsapp_id?: string | null;
  whatsapp?: string | null;
  wechat?: string | null;
  linkedin_url?: string | null;
  role_title?: string | null;
  job_title?: string | null;
  department?: string | null;
  role?: string | null;
  language?: string | null;
  preferred_language?: string | null;
  timezone?: string | null;
  is_primary: boolean;
  decision_role?: CRMContactDecisionRole | null;
  notes?: string | null;
  last_contacted_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListCRMContactsResponse {
  contacts: CRMContact[];
  total: number;
}

export interface CreateCRMContactRequest {
  account_id?: string | null;
  name: string;
  salutation?: string | null;
  email?: string | null;
  phone?: string | null;
  mobile?: string | null;
  whatsapp_id?: string | null;
  whatsapp?: string | null;
  wechat?: string | null;
  linkedin_url?: string | null;
  role_title?: string | null;
  job_title?: string | null;
  department?: string | null;
  role?: string | null;
  language?: string | null;
  preferred_language?: string | null;
  timezone?: string | null;
  is_primary?: boolean;
  decision_role?: CRMContactDecisionRole | null;
  notes?: string | null;
  last_contacted_at?: string | null;
}

export type UpdateCRMContactRequest = CreateCRMContactRequest;

export interface CRMAccountProfile {
  id: string;
  workspace_id: string;
  account_id: string;
  summary?: string | null;
  profile_json: Record<string, unknown>;
  updated_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface UpsertCRMAccountProfileRequest {
  summary?: string | null;
  profile_json?: Record<string, unknown>;
}

export type CRMCommunicationChannel = "manual" | "email" | "whatsapp" | "phone" | "meeting" | "other";
export type CRMCommunicationDirection = "inbound" | "outbound" | "note";

export interface CRMCommunicationNote {
  id: string;
  workspace_id: string;
  account_id?: string | null;
  contact_id?: string | null;
  channel: CRMCommunicationChannel;
  direction: CRMCommunicationDirection;
  occurred_at: string;
  subject?: string | null;
  body: string;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListCRMCommunicationNotesResponse {
  notes: CRMCommunicationNote[];
  total: number;
}

export interface CreateCRMCommunicationNoteRequest {
  contact_id?: string | null;
  channel?: CRMCommunicationChannel;
  direction?: CRMCommunicationDirection;
  occurred_at?: string | null;
  subject?: string | null;
  body: string;
}

export type CRMEmailThreadDirection = "inbound" | "outbound" | "mixed";
export type CRMEmailThreadStatus = "open" | "archived";
export type CRMEmailMessageDirection = "inbound" | "outbound";

export interface CRMEmailAttachment {
  filename?: string;
  content_type?: string;
  size_bytes: number;
  inline: boolean;
  content_id?: string;
  disposition?: string;
}

export interface CRMEmailThread {
  id: string;
  workspace_id: string;
  account_id?: string | null;
  contact_id?: string | null;
  project_id?: string | null;
  issue_id?: string | null;
  issue_ids?: string[];
  subject: string;
  external_thread_id?: string | null;
  mailbox?: string | null;
  direction: CRMEmailThreadDirection;
  status: CRMEmailThreadStatus;
  last_message_at?: string | null;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface CRMEmailAttachment {
  file_name: string;
  content_type?: string | null;
  content: string;
}

export interface CRMEmailMessage {
  id: string;
  workspace_id: string;
  thread_id: string;
  account_id?: string | null;
  contact_id?: string | null;
  external_message_id?: string | null;
  in_reply_to?: string | null;
  reference_ids?: string[];
  attachments?: CRMEmailAttachment[];
  sent_append_warning?: string | null;
  from_email?: string | null;
  from_name?: string | null;
  to_emails: string[];
  cc_emails: string[];
  bcc_emails: string[];
  subject?: string | null;
  sent_at?: string | null;
  received_at?: string | null;
  body_text?: string | null;
  body_html?: string | null;
  snippet?: string | null;
  raw_size_bytes?: number | null;
  in_reply_to?: string | null;
  reference_ids: string[];
  raw_headers?: Record<string, string[]> | null;
  attachments: CRMEmailAttachment[];
  direction: CRMEmailMessageDirection;
  created_at: string;
  updated_at: string;
}

export interface ListCRMEmailThreadsResponse {
  threads: CRMEmailThread[];
  total: number;
}

export interface CRMEmailThreadAssociationSuggestion {
  account_id: string;
  account_name: string;
  contact_id?: string | null;
  contact_name?: string | null;
  contact_email?: string | null;
  score: number;
  reasons: string[];
}

export interface ListCRMEmailThreadAssociationSuggestionsResponse {
  suggestions: CRMEmailThreadAssociationSuggestion[];
  total: number;
}

export interface ListCRMEmailMessagesResponse {
  messages: CRMEmailMessage[];
  total: number;
}

export interface CRMEmailEngineFolder {
  path: string;
  name: string;
  special_use?: string | null;
  total: number;
  unread: number;
}

export interface CRMEmailEngineStatus {
  enabled: boolean;
  configured: boolean;
  base_url?: string | null;
  account?: string | null;
  state?: string | null;
  syncing: boolean;
  last_error?: string | null;
  folders: CRMEmailEngineFolder[];
  fallback_provider: string;
}

export interface UpdateCRMEmailThreadAssociationRequest {
  account_id?: string | null;
  contact_id?: string | null;
  project_id?: string | null;
  issue_id?: string | null;
  issue_ids?: string[];
}

export interface CreateCRMEmailThreadRequest {
  account_id?: string | null;
  contact_id?: string | null;
  subject: string;
  external_thread_id?: string | null;
  mailbox?: string | null;
  direction?: CRMEmailThreadDirection;
  status?: CRMEmailThreadStatus;
  last_message_at?: string | null;
}

export interface CreateCRMEmailMessageRequest {
  account_id?: string | null;
  contact_id?: string | null;
  external_message_id?: string | null;
  in_reply_to?: string | null;
  reference_ids?: string[];
  attachments?: CRMEmailAttachment[];
  from_email?: string | null;
  from_name?: string | null;
  to_emails?: string[];
  cc_emails?: string[];
  bcc_emails?: string[];
  subject?: string | null;
  sent_at?: string | null;
  received_at?: string | null;
  body_text?: string | null;
  body_html?: string | null;
  snippet?: string | null;
  raw_size_bytes?: number | null;
  in_reply_to?: string | null;
  reference_ids?: string[];
  raw_headers?: Record<string, string[]> | null;
  attachments?: CRMEmailAttachment[];
  direction: CRMEmailMessageDirection;
}

export interface LinkCRMAccountProjectRequest {
  project_id?: string;
  project_ids?: string[];
  label?: string | null;
}

export interface LinkCRMAccountProjectsResponse {
  resources: ProjectResource[];
  total: number;
  skipped_project_ids: string[];
}

export interface CreateCRMFollowUpIssueRequest {
  project_id?: string | null;
  title?: string;
  description?: string | null;
  priority?: "none" | "low" | "medium" | "high" | "urgent";
  assignee_type?: "agent" | "member" | null;
  assignee_id?: string | null;
  due_date?: string | null;
}


export type CRMIMAPTLSMode = "ssl" | "starttls" | "none";

export interface CRMIMAPSetting {
  id: string;
  workspace_id: string;
  label: string;
  email: string;
  host: string;
  port: number;
  tls_mode: CRMIMAPTLSMode;
  username: string;
  secret_ref?: string | null;
  sync_enabled: boolean;
  last_test_status?: string | null;
  last_test_message?: string | null;
  last_tested_at?: string | null;
  owner_type?: string | null;
  owner_id?: string | null;
  smtp_host?: string | null;
  smtp_port?: number | null;
  smtp_tls_mode?: string | null;
  smtp_username?: string | null;
  smtp_secret_ref?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListCRMIMAPSettingsResponse {
  settings: CRMIMAPSetting[];
  total: number;
}

export interface UpsertCRMIMAPSettingRequest {
  id?: string | null;
  label: string;
  email: string;
  host: string;
  port: number;
  tls_mode: CRMIMAPTLSMode;
  username: string;
  secret_ref?: string | null;
  secret?: string | null;
  sync_enabled?: boolean;
  owner_type?: string | null;
  owner_id?: string | null;
  smtp_host?: string | null;
  smtp_port?: number | null;
  smtp_tls_mode?: string | null;
  smtp_username?: string | null;
  smtp_secret_ref?: string | null;
  smtp_secret?: string | null;
}

export interface CRMIMAPTestResponse {
  ok: boolean;
  status: string;
  message: string;
}

export interface CRMIMAPPreviewMessage {
  uid: string;
  external_message_id: string;
  in_reply_to?: string | null;
  reference_ids?: string[];
  subject: string;
  from_email: string;
  from_name: string;
  to_emails: string[];
  cc_emails: string[];
  received_at?: string | null;
  snippet: string;
  raw_size: number;
}

export interface CRMIMAPPreviewResponse {
  messages: CRMIMAPPreviewMessage[];
  total: number;
  limit: number;
  sync_enabled: boolean;
  note: string;
}

export interface CRMIMAPImportResponse {
  ok: boolean;
  run_id?: string;
  fetched: number;
  imported: number;
  skipped: number;
}

export interface CRMProfileSuggestion {
  id: string;
  workspace_id: string;
  account_id: string;
  summary?: string | null;
  profile_json: Record<string, unknown>;
  source_count: number;
  status: "draft" | "applied" | "dismissed";
  created_at: string;
  applied_at?: string | null;
}
