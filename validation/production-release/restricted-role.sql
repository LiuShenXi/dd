\set ON_ERROR_STOP on
-- Invoke with psql -v release_role=carpool_release_20260908. The password stays
-- in the process environment and is never a command-line argument or SQL log.
\getenv release_password CARPOOL_RELEASE_PASSWORD
BEGIN;
SET LOCAL log_statement = 'none';
CREATE ROLE :"release_role" LOGIN PASSWORD :'release_password'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
GRANT CONNECT ON DATABASE sub2api TO :"release_role";
GRANT USAGE ON SCHEMA public TO :"release_role";
GRANT SELECT ON ALL TABLES IN SCHEMA public TO :"release_role";
GRANT UPDATE (restrict_public_groups, updated_at) ON users TO :"release_role";
-- PostgreSQL row locks require an UPDATE privilege on at least one column.
GRANT UPDATE (updated_at) ON groups, api_keys TO :"release_role";
GRANT INSERT ON carpool_terms, carpool_cycles, carpool_ledger,
    carpool_billing_bindings, carpool_release_imports,
    auth_cache_invalidation_outbox TO :"release_role";
GRANT INSERT, DELETE ON user_allowed_groups TO :"release_role";
GRANT USAGE ON SEQUENCE carpool_terms_id_seq, carpool_cycles_id_seq,
    carpool_ledger_id_seq, auth_cache_invalidation_outbox_id_seq TO :"release_role";
COMMIT;
