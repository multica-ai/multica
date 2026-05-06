export interface WikiPage {
  id: string;
  workspace_id: string;
  title: string;
  content: string;
  parent_id: string | null;
  created_by_id: string;
  created_by_type: string;
  created_at: string;
  updated_at: string;
}

export interface CreateWikiPageRequest {
  title: string;
  content?: string;
  parent_id?: string;
}

export interface UpdateWikiPageRequest {
  title?: string;
  content?: string;
  parent_id?: string | null;
}

export interface ListWikiPagesResponse {
  pages: WikiPage[];
  total: number;
}
