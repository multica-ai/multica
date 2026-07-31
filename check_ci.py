import json
d = json.load(open('/tmp/multica-pr/upstream_ci.json'))
for r in d.get('check_runs', []):
    print(f'{r["name"]:50s} | {r["status"]:10s} | {r.get("conclusion") or "pending"}')
