ALTER TABLE channel_inbound_audit
ADD COLUMN workspace_id UUID;

-- Connector cutover preserves the legacy installation UUID. At migration
-- time each converted connector has exactly one workspace grant, so attribute
-- its existing non-content audit rows before multi-workspace grants can be
-- created by the new application version.
UPDATE channel_inbound_audit audit
SET workspace_id = (
    SELECT g.workspace_id
    FROM dingtalk_workspace_grant g
    WHERE g.connector_id = audit.installation_id
    LIMIT 1
)
WHERE audit.channel_type = 'dingtalk'
  AND audit.workspace_id IS NULL
  AND (
      SELECT count(*)
      FROM dingtalk_workspace_grant g
      WHERE g.connector_id = audit.installation_id
  ) = 1;
