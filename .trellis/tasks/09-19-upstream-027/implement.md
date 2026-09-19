# Implementation and verification

1. Verify completed 0.2.6/sentinel baseline against release reports and local image identity; merge exact v0.2.7 without disturbing older checkouts.
2. Review all 59 upstream files, especially routing/DI, plugin host/status, Seedance billing, header/mobile navigation and existing carpool restrictions.
3. Implement minimal compatibility guards and business regressions. Preserve all historical SQL and custom code outside necessary compatibility boundaries.
4. Run locked frontend install, typecheck, lint, tests and build. Run Go 1.27 unit suite, vet, server/CLI build, sentinel tests, and existing isolated PostgreSQL harness for migration/rollback/carpool/billing tests.
5. Independently review final changes; record source hashes and results; create the local merge commit. Deliver accurate source location and state. No production deployment or remote push in this request.
