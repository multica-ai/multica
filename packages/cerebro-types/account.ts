export interface Account {
  id: string;
  workspace_id: string;
  provider: string;
  login_identity: string;
  /** % of current usage window consumed, 0..100. Null when never reported. */
  usage_window_pct: number | null;
  /** RFC3339 timestamp when a 429 cooldown lifts. Null when not throttled. */
  throttled_until: string | null;
  /** User opted in to "allow extra spend" (Claude pro/max). */
  extra_spend_on: boolean;
  /** User manually paused this account from the UI. */
  paused_manual: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateAccountRequest {
  provider: string;
  login_identity: string;
}

export interface UpdateAccountControlsRequest {
  extra_spend_on?: boolean;
  paused_manual?: boolean;
}
