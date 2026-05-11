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

export interface CRMEmailThread {
  id: string;
  workspace_id: string;
  account_id?: string | null;
  contact_id?: string | null;
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

export interface CRMEmailMessage {
  id: string;
  workspace_id: string;
  thread_id: string;
  account_id?: string | null;
  contact_id?: string | null;
  external_message_id?: string | null;
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
  direction: CRMEmailMessageDirection;
  created_at: string;
  updated_at: string;
}

export interface ListCRMEmailThreadsResponse {
  threads: CRMEmailThread[];
  total: number;
}

export interface ListCRMEmailMessagesResponse {
  messages: CRMEmailMessage[];
  total: number;
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
