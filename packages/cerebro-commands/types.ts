export interface CerebroCommand {
  id: string;
  workspace_id: string;
  key: string;
  title: string;
  description: string;
  argv: string[];
  created_by_id: string;
  created_by_type: string;
  created_at: string;
  updated_at: string;
}

export type CommandInput = Pick<CerebroCommand, "key" | "title" | "description" | "argv">;
