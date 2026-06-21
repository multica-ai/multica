ALTER TABLE comment
    DROP COLUMN IF EXISTS chapter_id;

DROP TABLE IF EXISTS cerebro_chapters;
