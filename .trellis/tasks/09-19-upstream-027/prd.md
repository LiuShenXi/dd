# Incremental Sub2API 0.2.7 integration

Integrate upstream v0.2.7 into the already completed 0.2.6 custom release, preserving carpool, billing, branding, image fixes, schema rollback compatibility and the deployed ticket sentinel r2.

The authoritative baseline is 2fefcf51f0e00618686ab825d81e65444ee1ccfd (application f8f099c1d2851d1cfe7416e7f41a5108d529079c), from sub2api-sentinel-integration-20260919. The original dd checkout is stale and must not be used as the release baseline. Its dirty work is preserved.

Acceptance: retain complete baseline history; integrate exact upstream aea725f2ea644d5592d0bbb1d63b607efa7e200a; keep all 297 migration SQL files byte-identical; ensure new endpoints cannot bypass carpool billing; pass frontend typecheck/lint/tests/build, backend tests/vet/build, focused real PostgreSQL transaction tests and sentinel tests. Record evidence and validation limits. Source integration does not authorize production deployment or remote publication.
