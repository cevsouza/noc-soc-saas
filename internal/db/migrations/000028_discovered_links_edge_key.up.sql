-- 000028_discovered_links_edge_key.up.sql
--
-- Refines the discovered_links dedup key (topology slice B). 000027 keyed on
-- (tenant, local_ip, local_port, remote_chassis_id, remote_port_id), which made the REMOTE PORT part
-- of the edge identity: if a neighbour's port changed on a re-walk, a NEW row was inserted instead of
-- updating, and — with no pruning on this table — rewires would accumulate stale duplicate edges
-- forever. The real identity of an adjacency is (local device, local port, remote neighbour); the
-- remote port is a mutable attribute of that edge. Re-key so a re-walk updates the port in place.

DO $$
DECLARE
    c text;
BEGIN
    -- Drop whatever unique constraint 000027 created (its generated name is truncated/unstable).
    SELECT conname INTO c
    FROM pg_constraint
    WHERE conrelid = 'discovered_links'::regclass AND contype = 'u'
    LIMIT 1;
    IF c IS NOT NULL THEN
        EXECUTE format('ALTER TABLE discovered_links DROP CONSTRAINT %I', c);
    END IF;
END
$$;

ALTER TABLE discovered_links
    ADD CONSTRAINT discovered_links_edge_key
    UNIQUE (tenant_id, local_ip, local_port, remote_chassis_id);
