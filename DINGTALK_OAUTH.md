# DingTalk OAuth Login (Self-Hosted)

> English | [中文](DINGTALK_OAUTH.zh.md)

DingTalk (钉钉) login lets members of the DingTalk organization that owns the
app sign in to a self-hosted Multica instance by scanning a QR code — no email
verification code needed. The login account is the member's **enterprise
email** (企业邮箱) from that organization's address book, so an existing
email-code user is matched automatically by address; first-time users are
provisioned from the DingTalk profile (name + avatar).

The app must live inside the organization whose members should log in, and the
org must assign enterprise mailboxes. An app under a personal org cannot work
— see "How the login resolves the account email".

Configuration is instance-wide (one DingTalk app per deployment), mirroring how
`GOOGLE_CLIENT_ID` works — authentication happens before workspace selection,
so the provider config cannot be scoped per workspace.

## 1. Create / configure the DingTalk app

On the [DingTalk Open Platform](https://open.dingtalk.com/) create an
**enterprise internal app** (企业内部应用, app type "H5微应用" works), then:

1. **Login callback** — App details → **钉钉登录与分享** (DingTalk Login &
   Share) → add the callback URL:

   ```
   https://<your-multica-host>/auth/dingtalk/callback
   ```

   DingTalk requires the `redirect_uri` of every authorize request to match
   this entry on **scheme + hostname + port**. If Multica is served on a
   non-default port (e.g. behind a reverse proxy on `:6443`), the port must be
   part of the configured callback URL. A mismatch produces
   `redirect_uri参数错误` on the DingTalk consent page.

2. **Permissions** — App details → 权限管理 (Permission Management). Both are
   required; without them the login rejects every user:

   - **成员信息读** (`qyapi_get_member`, under 通讯录管理): needed by
     `getbyunionid` and `topapi/v2/user/get`. Without it both answer errcode
     88 / subcode 60011.
   - **企业员工手机号信息和邮箱等个人信息** (sensitive, under 通讯录管理):
     without it `v2/user/get` returns the member record with the `email` and
     `org_email` fields omitted entirely.
   - **通讯录个人信息读权限** (个人权限): `GET /v1.0/contact/users/me` checks
     this when called with the token from the scan (used for nickname,
     avatar and unionId).

3. Note the **Client ID** (AppKey) and **Client Secret** (AppSecret).

## 2. Configure Multica

In `.env` of the self-host deployment:

```ini
DINGTALK_CLIENT_ID=dingxxxxxxxx
DINGTALK_CLIENT_SECRET=<app-secret>

# Recommended: first-time DingTalk users are created only when their
# enterprise email domain matches, regardless of ALLOW_SIGNUP.
ALLOWED_EMAIL_DOMAINS=your-company.com
```

`docker-compose.selfhost.yml` passes both `DINGTALK_*` variables through to
the backend. Restart the stack to apply:

```bash
docker compose -f docker-compose.selfhost.yml up -d
```

The login page shows a "Continue with DingTalk" button automatically: the
backend exposes `dingtalk_client_id` in the public `/api/config` response only
when the client ID is configured, and the web app reads it at runtime — no
frontend rebuild needed.

## How the login resolves the account email

```
browser → https://login.dingtalk.com/oauth2/auth
          (scope: "openid corpid" — required, see troubleshooting)
        ← callback ?authCode=...&state=...
web app  → POST /auth/dingtalk {auth_code}
backend  → POST /v1.0/oauth2/userAccessToken          (code → token)
         → GET  /v1.0/contact/users/me                (nick/avatar/unionId only)
         → app token → getbyunionid → topapi/v2/user/get
                       (the app's own org member record: org_email)
         → findOrCreateUser(email) → session JWT + cookies
```

The login email is exactly one thing: the **`org_email` (企业邮箱) field on the
app-org member record** — the admin-assigned enterprise address. The
member-profile personal email is deliberately not read. A user who is not a
member of the app's org, or a member whose record has no `org_email`, is
rejected. There is no fallback email source, by design.

Why not `me.email`? The consent endpoint returns **account-level data that
crosses org boundaries** (verified against the live API): with the app
registered under an unrelated 1-member personal org, `me` still returned the
user's company-org enterprise email — and the mobile number in plaintext.
Using it would (a) let an app under any personal org log users in via an
address that org never assigned, and (b) key the login on a value whose
selection is undocumented for accounts with enterprise emails in more than one
org. `me` is therefore used for nickname, avatar and unionId only.

Practical consequences:

- **Company deployment (app inside the company org, permissions above):**
  works. The email is deterministic — always that org's record for that
  member. Only that org's members can log in.
- **Personal-org app (个人项目 / unverified org):** cannot work. The org has
  no enterprise mailboxes, so the member record carries no email and every
  login is rejected with 403. Create the app inside the organization whose
  members should log in.

Existing users (e.g. created earlier via email-code login) are matched by
address and log straight in.

## Troubleshooting

Every failure below was hit in a real deployment; the backend logs the raw
DingTalk error body (`docker logs <backend> | grep -i dingtalk`), so check
there first.

| Symptom | Cause | Fix |
|---|---|---|
| Consent page: `redirect_uri参数错误` | Callback URL mismatch on scheme / hostname / **port** | Configure the exact URL (incl. port) in 钉钉登录与分享 |
| Callback shows 404 | `/auth/dingtalk/callback` is a frontend page; a proxy or rewrite that forwards all `/auth/*` to the backend breaks it | Keep the path on the frontend (see `isBackendAuthPath` in `apps/web/config/runtime-urls.ts`) |
| `DingTalk account has no unionId` | Authorize URL asked for `scope=openid` only; DingTalk gates `unionId` on scope | The built login page requests `scope=openid corpid` (space `%20`-encoded) — make sure your frontend build includes it |
| Backend log: `缺少参数：x-acs-dingtalk-access-token` | DingTalk v1.0 APIs use a custom header, not `Authorization: Bearer` | Handled by the implementation (`x-acs-dingtalk-access-token`) |
| `DingTalk login is unavailable ... contact sensitive permission` | `getbyunionid`/`v2/user/get` rejected: the 成员信息读 (`qyapi_get_member`) permission is missing, or the user is not a member of the app's org | Grant the permission (one-click link in the backend log); non-members are out of scope by design |
| `DingTalk login is unavailable: the app's organization has no email on record` | The member record has no email — typically an app under a personal org (no enterprise mailboxes), or the sensitive permission is missing so the fields are omitted | Use an app inside the org that assigns enterprise mail; grant 「企业员工手机号信息和邮箱等个人信息」 |
| `不合法的临时授权码` (invalid authCode) | The one-time `authCode` was replayed — e.g. the callback page was refreshed | Not a bug; start a fresh login from the login page |

## Security notes

- The client secret never leaves the backend container; the web app only
  receives the public client ID via `/api/config`.
- `/auth/dingtalk` is registered behind the same auth rate-limit middleware as
  `/auth/send-code` (`authRL`). On deployments without Redis this backend
  limiter is a no-op — put an nginx `limit_req` on `/auth/` as with the other
  auth endpoints (see `SELF_HOSTING.md`).
- `ALLOWED_EMAIL_DOMAINS` is the recommended gate for first-time users when
  `ALLOW_SIGNUP=false`: existing users always log in, new users are created
  only from allow-listed enterprise email domains.

## Implementation map

| Piece | File |
|---|---|
| OAuth exchange + email resolution + session | `server/internal/handler/dingtalk_login.go` |
| Route `POST /auth/dingtalk` | `server/cmd/server/router.go` |
| `dingtalk_client_id` in `/api/config` | `server/internal/handler/config.go` |
| Env passthrough | `docker-compose.selfhost.yml` |
| Login-page button + authorize URL | `packages/views/auth/login-page.tsx` |
| Callback page | `apps/web/app/auth/dingtalk/callback/page.tsx` |
| Frontend-route exception for the callback | `apps/web/config/runtime-urls.ts` |
