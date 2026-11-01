-- Floors of the digital company office. A floor is a named, ordered group of
-- six squad rooms (slots 0..5, 3 x 2); floors are the tabs of the office page.
-- A squad takes a seat on a floor through squad.floor_id and squad.office_slot
-- (migration 605).
--
-- No foreign keys: squad.floor_id and workspace_id are application-layer
-- relations. Deleting a floor requires it to hold no live squad, and workspace
-- deletion removes the rows explicitly.
--
-- id is not declared PRIMARY KEY inline. Following the repo convention (327-334,
-- 597-599) the table is built without one, the backing unique index is created
-- CONCURRENTLY in 602, and 603 attaches it with PRIMARY KEY USING INDEX.
CREATE TABLE office_floor (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
