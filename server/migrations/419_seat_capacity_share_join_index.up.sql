-- One user joining through one share link must keep one Cloud operation until
-- that operation is settled, including while it is dead-lettered or releasing.
CREATE UNIQUE INDEX CONCURRENTLY idx_seat_capacity_outbox_share_join
    ON seat_capacity_outbox(workspace_id, share_link_id, user_id)
    WHERE share_link_id IS NOT NULL AND user_id IS NOT NULL;
