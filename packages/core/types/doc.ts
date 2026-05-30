export interface Doc {
  id: string;
  workspace_id: string;
  creator_id: string;
  title: string;
  content: string;
  parent_id: string | null;
  position: number;
  is_archived: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateDocRequest {
  title: string;
  content?: string;
  parent_id?: string | null;
  position?: number;
}

export interface UpdateDocRequest {
  title?: string;
  content?: string;
  parent_id?: string | null;
  position?: number;
}
