-- 000039_playbooks.up.sql
--
-- Multi-step SOAR playbook engine (Backlog B7). A `playbook` is a named, ordered sequence of
-- abstract steps (notify / comment / response_action). A `playbook_run` is one execution of a
-- playbook, optionally bound to an incident, carrying a `context` map (e.g. the source IP to act
-- on). `playbook_run_steps` is the per-step audit trail. Steps that mutate network/endpoint state
-- (response_action) pause the run for human approval — the same human-in-the-loop guarantee the
-- single-action containment queue (response_action_requests) already enforces.

CREATE TABLE IF NOT EXISTS playbooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,   -- ordered array of {type, ...params}
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS playbook_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    playbook_id UUID NOT NULL REFERENCES playbooks(id) ON DELETE CASCADE,
    incident_id UUID,                            -- nullable: a run can be standalone
    status VARCHAR(30) NOT NULL DEFAULT 'running', -- running, awaiting_approval, completed, failed, rejected
    current_step INT NOT NULL DEFAULT 0,           -- index of the next/paused step
    context JSONB NOT NULL DEFAULT '{}'::jsonb,     -- resolved values (e.g. {"src_ip":"1.2.3.4"})
    started_by VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS playbook_run_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES playbook_runs(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    step_index INT NOT NULL,
    step_type VARCHAR(50) NOT NULL,               -- notify, comment, response_action
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(30) NOT NULL DEFAULT 'pending', -- pending, succeeded, failed, awaiting_approval, skipped
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- RLS: force isolation on all three (they are tenant-scoped).
ALTER TABLE playbooks ENABLE ROW LEVEL SECURITY;
ALTER TABLE playbooks FORCE ROW LEVEL SECURITY;
ALTER TABLE playbook_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE playbook_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE playbook_run_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE playbook_run_steps FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON playbooks
    FOR ALL TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

CREATE POLICY tenant_isolation_policy ON playbook_runs
    FOR ALL TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

CREATE POLICY tenant_isolation_policy ON playbook_run_steps
    FOR ALL TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

CREATE INDEX IF NOT EXISTS idx_playbooks_tenant ON playbooks(tenant_id);
CREATE INDEX IF NOT EXISTS idx_playbook_runs_tenant_status ON playbook_runs(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_playbook_run_steps_run ON playbook_run_steps(run_id, step_index);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON playbooks TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON playbook_runs TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON playbook_run_steps TO noc_app_runtime;
    END IF;
END
$$;
