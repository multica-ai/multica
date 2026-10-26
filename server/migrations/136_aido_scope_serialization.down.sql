-- A downgrade must not silently restore the known privilege escalation.
DO $$ BEGIN
    RAISE EXCEPTION 'AIDO scope serialization security fix cannot be rolled back';
END $$;
