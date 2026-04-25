UPDATE issue
SET completed_at = updated_at
WHERE status IN ('done', 'cancelled')
  AND completed_at IS NULL;
