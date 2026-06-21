-- Revert FIR-1587: restore the strict user-only FK on proposed_by.
-- NOTE: this will fail if any change request was proposed by an agent (an id
-- not present in "user"); delete or remap those rows before rolling back.
ALTER TABLE skill_change_request
    ADD CONSTRAINT skill_change_request_proposed_by_fkey
    FOREIGN KEY (proposed_by) REFERENCES "user"(id) ON DELETE CASCADE;
