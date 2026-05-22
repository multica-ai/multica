# pr-agent-sidecar test trigger

Throwaway file used to open a PR and validate the end-to-end pr-agent webhook
flow:

```
GitHub PR open → Cloudflare Tunnel → pr-agent-sidecar → Multica issue created
                                                      → pr-reviewer agent assigned
```

Safe to delete after the corresponding Multica issue is verified.
