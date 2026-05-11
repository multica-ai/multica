export interface Account {
  id: string;
  workspace_id: string;
  provider: string;
  login_identity: string;
  created_at: string;
  updated_at: string;
}

export interface CreateAccountRequest {
  provider: string;
  login_identity: string;
}
