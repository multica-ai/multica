-- Dropping the gate means the delivery path finds no row and falls back to
-- legacy-only writes, which is the same behaviour as the gate being off.
DROP TABLE IF EXISTS inbox_v2_cutover;
