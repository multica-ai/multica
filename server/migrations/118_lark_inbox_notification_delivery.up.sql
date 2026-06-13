CREATE TABLE lark_inbox_notification_delivery (
    inbox_item_id UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    installation_id UUID NOT NULL REFERENCES lark_installation(id) ON DELETE CASCADE,
    lark_open_id TEXT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (inbox_item_id, installation_id, lark_open_id)
);
