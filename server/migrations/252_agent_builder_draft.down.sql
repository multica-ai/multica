-- Rolling back returns the studio to replaying the last <agent_draft> block out
-- of chat_message, which is where the AI's half of the configuration still
-- lives. Only edits the user typed and never sent are lost.
DROP TABLE IF EXISTS agent_builder_draft;
