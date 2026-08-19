CREATE UNIQUE INDEX CONCURRENTLY tag_http_assertion_replay_identity_uidx ON tag_http_assertion_replay(issuer, audience, request_id, nonce);
