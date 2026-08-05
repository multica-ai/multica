# Autopilot webhook contract fixtures

Contract scenarios covered by handler tests:

- invalid HMAC (`TestWebhookHandler_InvalidSignatureReturns401AndPersistsRejected`)
- missing HMAC when secret configured (`TestWebhookHandler_MissingSignatureReturns401WhenSecretSet`)
- delivery replay / dedupe (`TestWebhookHandler_DedupeViaGitHubDelivery`, `TestReplay_CreatesNewDeliveryAndDispatchesRun`)
- concurrent scope claim (`TestWebhookHandler_ScopeClaimReturnsActiveRun`)
- head SHA refresh on active scope (`TestWebhookHandler_ScopeClaimUpdatesHeadSHA`)
- payload redaction (`TestBuildStoredWebhookEnvelope_OmitsRawComment`)
- worker lease expiry recovery (`TestWebhookDeliveryWorker_RecoversExpiredLeaseAndReusesRun`)

Use these fixtures when extending ingress without copying raw GitHub payloads into agent prompts.
