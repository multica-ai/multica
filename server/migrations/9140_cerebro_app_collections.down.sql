DROP FUNCTION IF EXISTS cerebro_app_folder_grant_visible(uuid, uuid);

DELETE FROM cerebro_folder_grant WHERE surface = 'app';

ALTER TABLE cerebro_folder_grant
    DROP CONSTRAINT IF EXISTS cerebro_folder_grant_surface_check;

ALTER TABLE cerebro_folder_grant
    ADD CONSTRAINT cerebro_folder_grant_surface_check
    CHECK (surface IN ('artifact', 'entity'));
