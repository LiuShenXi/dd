# Frontend asynchronous identity verification

Working directory: `C:\WORK-SPACE\sub2api-carpool-v1.3\frontend`

Command:

```powershell
pnpm exec eslint src/api/admin/carpool.ts src/api/__tests__/admin.carpool.spec.ts src/components/admin/user/CarpoolTermModal.vue src/components/admin/user/__tests__/CarpoolTermModal.spec.ts src/views/admin/CarpoolView.vue src/views/admin/__tests__/CarpoolView.async.spec.ts
pnpm typecheck
pnpm exec vitest run src/api/__tests__/admin.carpool.spec.ts src/components/admin/user/__tests__/CarpoolTermModal.spec.ts src/views/admin/__tests__/CarpoolView.async.spec.ts
```

Exit code: `0`

Output:

```text
> sub2api-frontend@1.0.0 typecheck C:\WORK-SPACE\sub2api-carpool-v1.3\frontend
> vue-tsc --noEmit

PASS src/api/__tests__/admin.carpool.spec.ts (7 tests)
PASS src/components/admin/user/__tests__/CarpoolTermModal.spec.ts (9 tests)
PASS src/views/admin/__tests__/CarpoolView.async.spec.ts (6 tests)

Test Files  3 passed (3)
Tests       22 passed (22)
```

Covered cases: submit completion after a user switch, stale admin-list refresh ordering, delayed payment completion after switching targets, closing/clearing a successful payment before the list refresh resolves, cross-dialog stale success/error isolation, nullable empty-list normalization at the API boundary, superseded same-tier renewal mapping, and disabled-plan exclusion without cross-tier guessing.
