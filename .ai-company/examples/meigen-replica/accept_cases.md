# Acceptance Cases — meigen-replica

## TICKET-001 — Harness + agent-delivery CI

- [x] AC-H1: `make check` exits 0
- [x] AC-H2: `make visual-check` exits 0
- [x] AC-H3: CI runs `make check` + `make visual-check` on PR
- [x] AC-H4: GitHub agent labels exist
- [x] AC-H5: `scripts/agent-delivery/*.sh` present

## Structure / interaction

- [x] AC-S1: `/#studio` shows studio + dock
- [x] AC-S2: Gallery Generate → Studio with prefilled prompt
- [x] AC-S3: Skills wizard Apply to dock
- [x] AC-S4: zh/en locale toggle

## Visual

- [ ] AC-V1: home-desktop screenshot pass (`make visual-check`)
- [ ] AC-V2: home-mobile-375 screenshot pass
- [ ] AC-V3: break H1 → visual-check fails; restore → pass

## Verification

```bash
make check
make visual-check
```
