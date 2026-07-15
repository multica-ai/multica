# Mini-app SDK

Load `/api/cerebro/apps-runtime/sdk/multica.js` as an ES module and call `createMulticaApp({ appId, version })`. The SDK sends browser credentials only to Multica routes and throws a user-safe `Error` for non-success responses.

## Public methods

| Method | Purpose | Server route |
|---|---|---|
| `registry.token()` | Mint a short-lived Registry key within the approved app and human scope ceiling. | `POST /api/cerebro/apps/{appId}/token` |
| `storage.get(key)` | Read one app-owned JSON value. | `GET /api/cerebro/apps/{appId}/storage/{key}` |
| `storage.set(key, value)` | Create or replace one app-owned JSON value. | `PUT /api/cerebro/apps/{appId}/storage/{key}` |
| `storage.delete(key)` | Delete one app-owned value. | `DELETE /api/cerebro/apps/{appId}/storage/{key}` |
| `connections.call(connectionId, tool, arguments)` | Call one configured endpoint when both the app grant and viewing human permit it. | `POST /api/cerebro/connections/{connectionId}/call` |
| `workers.invoke(input)` | Invoke this immutable app version's isolated backend. | `POST /api/cerebro/apps-runtime/workers/{appId}/{version}/invoke` |
| `views.submit(viewId, value, requestId)` | Submit a response for one waiting interactive workflow request. | `POST /api/cerebro/apps/{appId}/views/{viewId}/submissions` |
| `views.onInput(handler)` | Receive the view input and request ID from the sandbox host. | Browser `message` event |

Do not persist Registry keys, Connection credentials, or copied data outside the app-owned storage API. Do not call internal services directly; use the SDK so the server can enforce both ceilings and audit the action.

