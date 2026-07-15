# Mini-app manifest

`app.json` contains a `manifest` with this stable shape:

```json
{
  "manifest": {
    "schema_version": "1",
    "name": "Allergen Formatter",
    "version": "1.0.0",
    "scopes": [
      { "resource_type": "data_source", "resource_id": "products", "access": "read" }
    ],
    "frontend": { "entry": "frontend/index.html" },
    "backend": { "entry": "backend/index.mjs" },
    "views": [
      { "id": "formatter", "type": "form", "title": "Format allergens" }
    ]
  }
}
```

Versions use semantic versioning and are immutable after publishing. Supported view types are `form`, `lookup`, and `approval`. Supported scope resources are `data_source`, `data_destination`, `app`, `function`, `integration`, and `bigquery_credential`; access is `read`, `write`, or `read_write`. Request the smallest exact set. Publishing a new version requires a new scope review.

