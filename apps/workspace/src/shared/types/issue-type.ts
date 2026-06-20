export type IssueTypeLoadProfile = "deep_work" | "light_work" | "recovery" | "neutral";

export interface IssueType {
  id: string;
  workspace_id: string;
  key: string;
  name: string;
  description: string;
  color: string;
  icon: string;
  load_profile: IssueTypeLoadProfile;
  is_system: boolean;
  archived_at: string | null;
  position: number;
  created_at: string;
  updated_at: string;
}
