# Client Review Widget

A comment layer for dev sites. The client opens a signed review link, clicks any
element on the page, and leaves a comment. It lands in that project's Multica
workspace as an assigned issue with a screenshot of the element.

Comments only. This is not a visual editor.

## Pieces

| Path | What it is |
|---|---|
| `static/review.js` | The widget. Vanilla JS, no build step, no secrets. |
| `src/server.js` | Ingest service. Holds the `mul_` token, verifies review links. |
| `bin/mint-review-link.js` | Issues a signed review link for a client. |
| `projects.json` | Maps a project slug to its workspace, project and assignee. |

## How it fits together

```
client browser  ──►  ingest service  ──►  Multica API
  review.js            (godfinns)          (localhost:8090)
  + snapdom            holds mul_ token     LAN-only, never public
```

The widget holds no credentials. It knows the ingest URL and the review token
from the query string, nothing else. The ingest service is the trust boundary:
it verifies the HMAC-signed token, and can only ever create issues in the
workspace that token maps to.

## Setup

```bash
cp .env.dev .env          # then fill in MULTICA_TOKEN and REVIEW_SECRET
openssl rand -hex 32      # → REVIEW_SECRET
./run.sh                  # foreground, or ./run.sh --bg
```

`MULTICA_TOKEN` is a `mul_` personal access token. Get it from the Multica CLI
config or mint a new one. Never commit it.

Edit `projects.json` to map each project:

```json
{
  "ips": {
    "workspace_id": "…",
    "project_id": "…",
    "base_url": "https://dart.hbhf.is"
  }
}
```

### Client comments land in `backlog`, unassigned — on purpose

A client comment is a **request**, not an approved work order. Issues are created
in `backlog` with no assignee, which is the one state the Multica handoff
contract allows to be unassigned and the one the daemon will not dispatch from.
Triage them yourself, then assign and move to `todo` when the work is approved.

**Do not add `assignee_id` to a project here unless you mean it.** Assignment
plus `todo` is exactly what makes the daemon pick an issue up. An earlier version
of this service created assigned `todo` issues, and agents opened real PRs
against the live ips repo within seconds of a comment being posted (PR #264,
from a *test* comment, reached CI before it was caught). Optional overrides
`assignee_type` / `assignee_id` / `status` exist for projects that genuinely want
auto-dispatch, but the default is deliberately inert.

## Issuing a review link

```bash
node bin/mint-review-link.js ips "Jón at IPS" --days 30
```

Prints a link like `https://dart.flexmedia.is/?review=<token>`. Send it to the
client. The token carries the project, the client's name (stamped on every
issue) and an expiry. Revoke everything by rotating `REVIEW_SECRET`.

## Adding the widget to a site

The widget must be served **same-origin** with the page. A strict CSP
(`script-src 'self'`) will refuse a script from another host, which is the case
on ips today. So copy the file rather than hotlinking it:

```bash
cp static/review.js       <site>/static/review.js
cp static/snapdom.js      <site>/static/snapdom.js
```

Then include it, gated so it can never reach production:

```html
<!-- dev only -->
<script src="/snapdom.js"></script>
<script src="/review.js" data-ingest="https://review-ingest.flexmedia.is"></script>
```

If the site's CSP also restricts `connect-src`, either add the ingest host to it
or proxy `/ingest/*` through the app so everything stays same-origin.

The widget is inert without `?review=<token>` in the URL — no launcher, no
network calls — so an accidental include is a no-op rather than a leak.

## Screenshots

Capture uses [snapdom](https://github.com/zumerlab/snapdom). It must be loaded
before `review.js`; without it comments still work, they just carry no image.

A hand-rolled SVG `foreignObject` capture is **not** a viable fallback: Chromium
taints the canvas and `toDataURL` throws, so it can never produce an image.
That was tried and removed.

Known limits: cross-origin images, iframes and WebGL may render blank. For a
"which element, and what did it look like" use case that is fine. If fidelity
ever matters, a server-side Playwright capture is the escape hatch.

## Endpoints

| Route | Purpose |
|---|---|
| `POST /comment` | Comment → assigned Multica issue + screenshot |
| `GET /pins?token=&path=` | Existing review issues for a page, so pins redraw |
| `GET /health` | Status, configured projects |

Every route requires a valid signed token. Comments are rate-limited to 20 per
token per 10 minutes.

## Notes

- Review issues are marked with an HTML comment in the description so `/pins`
  can tell them apart from dev-generated issues.
- The review token is stripped from the page URL before it is written into an
  issue, so a working credential never lands on the board or in a GitHub sync.
- Uploads go through `POST /api/upload-file` with an `issue_id` form field.
  `POST /api/issues/{id}/attachments` does not exist (that path is GET-only and
  returns 405).
