-- Add full-text search support to issues.

ALTER TABLE issue ADD COLUMN search_vector tsvector;

-- Backfill existing rows.
UPDATE issue SET search_vector =
  setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
  setweight(to_tsvector('simple', coalesce(description, '')), 'B');

-- GIN index for fast full-text search.
CREATE INDEX idx_issue_search ON issue USING GIN(search_vector);

-- Auto-update trigger: keeps search_vector in sync when title/description change.
CREATE FUNCTION issue_search_vector_update() RETURNS trigger AS $$
BEGIN
  NEW.search_vector :=
    setweight(to_tsvector('simple', coalesce(NEW.title, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(NEW.description, '')), 'B');
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_issue_search_vector
  BEFORE INSERT OR UPDATE OF title, description ON issue
  FOR EACH ROW EXECUTE FUNCTION issue_search_vector_update();
