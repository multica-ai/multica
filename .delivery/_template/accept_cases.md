# Acceptance cases

<!-- 每条必须可测试。Verifier 和人都对照这份清单。禁止口头「很像」。 -->

## Prerequisites (replica / landing)

- [ ] `competitor_inventory.md` present
- [ ] `wont_do.md` present
- Missing either → **NEED_CLARIFY** / **BLOCKED**

## Structure

- [ ] Inventory MVP routes exist
- [ ] Inventory components locatable

## Interaction

- [ ] I-load / I-cta / I-mobile behave as inventory

## Visual

- [ ] `make visual-check` passes (`pnpm exec playwright test --grep @visual`)

## Functional

- [ ] <!-- 正常路径 -->

## Error / edge

- [ ] <!-- 参数缺失、权限、边界 -->

## Verification commands

- [ ] `pnpm typecheck` passes (if TS touched)
- [ ] `pnpm test --filter=<package>` passes (name the package)
- [ ] `make test` passes (if Go touched)
- [ ] `make check` passes before opening PR
- [ ] `make visual-check` passes for replica/landing tickets

## Max iterations

If verification still fails after **3** fix loops → mark **BLOCKED** and output `NEED_CLARIFY` or list blockers.
