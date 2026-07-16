# Mini-app SDK

Load `/api/cerebro/apps-runtime/sdk/multica.js` as an ES module and call `createMulticaApp({ appId, version })`.

The app runs in an opaque iframe and cannot use the signed-in browser session directly. The SDK sends a typed `postMessage` request to the trusted Apps host. The host verifies the iframe source, app ID, version, request ID, and method before it makes the authenticated Multica request. Replies are correlated by request ID and SDK failures become user-safe `Error` values inside the app.

## Public methods

| Method | Purpose | Server route |
|---|---|---|
| `registry.token()` | Mint a short-lived Registry key within the approved app and human scope ceiling. | `POST /api/cerebro/apps/{appId}/token` |
| `storage.get(key)` | Read one app-owned JSON value. | `GET /api/cerebro/apps/{appId}/storage/{key}` |
| `storage.set(key, value)` | Create or replace one app-owned JSON value. | `PUT /api/cerebro/apps/{appId}/storage/{key}` |
| `storage.delete(key)` | Delete one app-owned value. | `DELETE /api/cerebro/apps/{appId}/storage/{key}` |
| `connections.call(connectionId, tool, arguments)` | Call one configured endpoint when both the app grant and viewing human permit it. | `POST /api/cerebro/connections/{connectionId}/call` |
| `workers.invoke(input)` | Invoke this immutable app version's isolated backend as the viewing member. | `POST /api/cerebro/apps/{appId}/invoke` |
| `views.submit(viewId, value, requestId)` | Submit a response for one waiting interactive workflow request. | `POST /api/cerebro/apps/{appId}/views/{viewId}/submissions` |
| `views.onInput(handler)` | Receive the view input and request ID from the sandbox host. | Browser `message` event |

Only the methods in this table are accepted by the host. Unknown methods and requests claiming another app identity are ignored.

Do not persist Registry keys, Connection credentials, or copied data outside the app-owned storage API. Do not call internal services directly; use the SDK so the host and server can enforce both ceilings and audit the action.

The member-bound invoke route mints a short-lived invocation grant and signs the private runtime request. An app backend can then use its approved `multica.registry.call` and `multica.connections.call` host methods without receiving system credentials. The built-in Allergen Formatter uses the approved `ai_gateway` integration through this person-bound path.
