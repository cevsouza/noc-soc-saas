-- 000038_oncall.up.sql
--
-- On-call scheduling (B5 slice 1). A tenant defines named on-call SCHEDULES (rotations); each
-- schedule has SHIFTS assigning a user to cover a time window [starts_at, ends_at). "Who is on-call
-- now" for a schedule is the shift whose window contains NOW() (latest starts_at wins on overlap).
-- This slice is schedule management + visibility only; person-level routing/escalation-to-next is a
-- future slice (needs per-user contact + an escalation policy). Both tables are tenant-scoped (FORCE
-- RLS), mirroring every other tenant table.

CREATE TABLE IF NOT EXISTS oncall_schedules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oncall_schedules_tenant ON oncall_schedules (tenant_id);

CREATE TABLE IF NOT EXISTS oncall_shifts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    schedule_id UUID NOT NULL REFERENCES oncall_schedules(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oncall_shifts_lookup ON oncall_shifts (tenant_id, schedule_id, starts_at);

ALTER TABLE oncall_schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE oncall_schedules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON oncall_schedules
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

ALTER TABLE oncall_shifts ENABLE ROW LEVEL SECURITY;
ALTER TABLE oncall_shifts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON oncall_shifts
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON oncall_schedules TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON oncall_shifts TO noc_app_runtime;
    END IF;
END
$$;
