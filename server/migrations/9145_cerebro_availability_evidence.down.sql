-- Reverses 9145_cerebro_availability_evidence. The canonical capability and
-- alias rows are left in place: they belong to the 9144 catalog namespace, and
-- dropping identity here would delete a name other evidence may reference.
DROP TABLE IF EXISTS cerebro_availability_evidence;
