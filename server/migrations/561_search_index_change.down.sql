SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

DROP TRIGGER IF EXISTS trg_project_search_index_change ON project;
DROP TRIGGER IF EXISTS trg_comment_update_search_index_change ON comment;
DROP TRIGGER IF EXISTS trg_comment_search_index_change ON comment;
DROP TRIGGER IF EXISTS trg_issue_search_index_change ON issue;
DROP FUNCTION IF EXISTS record_search_index_change();
DROP TABLE IF EXISTS search_index_prune_mark;
DROP TABLE IF EXISTS search_index_change;
