-- Revert FIR-2998: remove the pending GLM-5.2 model-registry change request.
-- Only removes the pending proposal this migration created; an already-merged
-- change request (status 'merged') is left intact (it is now history).

BEGIN;

DELETE FROM model_registry_change_request
WHERE status = 'pending'
  AND proposed_snapshot -> 'models' ? 'glm-5.2'
  AND proposed_by = 'cb01aff2-2305-480a-b18c-a450a0ec22de';

COMMIT;
