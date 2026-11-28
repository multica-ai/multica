CREATE TABLE github_pr_review_delivery (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('pull_request_review', 'pull_request_review_comment')),
    github_event_id bigint NOT NULL,
    comment_id uuid NOT NULL,
    triggered_task_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE github_pr_review_wakeup_state (
    issue_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    consecutive_rounds integer NOT NULL DEFAULT 0,
    active_round_task_id uuid,
    active_round_review_id bigint,
    last_triggered_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);
