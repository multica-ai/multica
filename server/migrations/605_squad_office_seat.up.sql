-- A squad's seat on the office: a floor and one of its six room slots. Both are
-- set or both are NULL (enforced in the handler); a squad with no seat is
-- unseated and is not drawn. floor_id is an application-layer relation to
-- office_floor, never a foreign key. One live squad per seat is enforced by the
-- partial unique index built in 606.
ALTER TABLE squad ADD COLUMN floor_id UUID;
ALTER TABLE squad ADD COLUMN office_slot SMALLINT CHECK (office_slot IS NULL OR office_slot BETWEEN 0 AND 5);
