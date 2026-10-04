-- Tenant for RLS. NULL when unset, so nothing matches.
-- NULLIF: after a SET LOCAL ends, the setting reads '' rather than NULL.

-- +goose Up
CREATE FUNCTION current_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid $$;

-- +goose Down
DROP FUNCTION current_tenant_id();
