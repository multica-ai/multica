export interface WikiPageSummary {
  id: string;
  workspace_id: string;
  parent_id: string | null;
  title: string;
  slug: string;
  position: number;
  created_by: string | null;
  updated_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface WikiPage extends WikiPageSummary {
  content: string;
}

export interface ListWikiPagesResponse {
  pages: WikiPageSummary[];
  total: number;
}

export interface CreateWikiPageRequest {
  title: string;
  parent_id?: string | null;
  content?: string;
  position?: number;
}

export interface UpdateWikiPageRequest {
  title?: string;
  content?: string;
  position?: number;
}

export interface ReorderWikiPagesRequest {
  pages: Array<{ id: string; position: number }>;
}
