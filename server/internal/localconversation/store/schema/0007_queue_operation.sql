CREATE TABLE queue_operation (
  conversation_id TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  action TEXT NOT NULL,
  request_id TEXT NOT NULL,
  PRIMARY KEY (conversation_id, operation_id)
);
