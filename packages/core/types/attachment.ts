export interface Attachment {
  id: string;
  workspace_id: string;
  issue_id: string | null;
  comment_id: string | null;
  uploader_type: string;
  uploader_id: string;
  filename: string;
  url: string;
  download_url: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
}
