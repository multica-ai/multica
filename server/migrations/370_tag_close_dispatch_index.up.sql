CREATE UNIQUE INDEX CONCURRENTLY tag_access_connection_close_dispatch_command_uidx
    ON tag_access_connection_close_dispatch (command_id);
