# Integration secret re-key runbook

Replaces an integration's `MULTICA_*_SECRET_KEY` without breaking stored
connections. Covers Feishu/Lark, Slack, DingTalk, WeCom, WeChat, Telegram,
Connected Apps, and VCS. `MULTICA_PLUGIN_SECRET_KEY` is not covered.

1. Generate a new 32-byte key per integration (`openssl rand -base64 32`).
2. Deploy every server with the new value in `MULTICA_<NAME>_SECRET_KEY` and
   the old value in `MULTICA_<NAME>_SECRET_KEY_PREVIOUS`. Servers then seal
   with the new key and open values sealed under either key.
3. From `server/`, with the same key env and `DATABASE_URL`, report what is
   still under the old key, then re-seal it:

   ```bash
   go run ./cmd/rekey_secrets            # report only
   go run ./cmd/rekey_secrets --apply    # re-seal
   go run ./cmd/rekey_secrets            # verify: exits 0 when nothing remains
   ```

   Each row is re-sealed in its own transaction, so the command is safe to
   interrupt and rerun. A value that opens under neither key is logged with
   its row key and left untouched.
4. Remove `MULTICA_<NAME>_SECRET_KEY_PREVIOUS` and redeploy only after the
   report run exits 0.
