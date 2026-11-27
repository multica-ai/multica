# Integration secret re-key runbook

Replaces an integration's `MULTICA_*_SECRET_KEY` without breaking stored
connections. Covers Feishu/Lark, Slack, DingTalk, WeCom, WeChat, Telegram,
Connected Apps, and VCS. `MULTICA_PLUGIN_SECRET_KEY` is not covered.

1. Generate a new 32-byte key per integration (`openssl rand -base64 32`).
2. Deploy every server with the new value in `MULTICA_<NAME>_SECRET_KEY` and
   the old value in `MULTICA_<NAME>_SECRET_KEY_PREVIOUS`. Servers then seal
   with the new key and open values sealed under either key.
3. From `server/`, with the same key env and `DATABASE_URL`, report what is
   still under the old key, then re-seal it. `--integrations` names the
   integrations being rotated (`lark,slack,dingtalk,wecom,weixin,telegram,apps,vcs`
   or `all`); each one's current key must be set, or the command fails before
   scanning.

   ```bash
   go run ./cmd/rekey_secrets --integrations=all            # report only
   go run ./cmd/rekey_secrets --integrations=all --apply    # re-seal
   go run ./cmd/rekey_secrets --integrations=all            # verify: exits 0 when nothing remains
   ```

   Each row is re-sealed in its own transaction, so the command is safe to
   interrupt and rerun. A value that opens under neither key is logged with
   its row key and left untouched.
4. Remove `MULTICA_<NAME>_SECRET_KEY_PREVIOUS` and redeploy only after the
   verify run exits 0 and at least 10 minutes have passed since the last
   server running with only the old key stopped. Connected Apps OAuth state
   travels in the browser, not the database, and stays valid for 10 minutes;
   an authorization started on such a server fails its callback once the old
   key is gone.
