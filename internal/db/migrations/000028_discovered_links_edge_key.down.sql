-- 000028_discovered_links_edge_key.down.sql
ALTER TABLE discovered_links DROP CONSTRAINT IF EXISTS discovered_links_edge_key;
ALTER TABLE discovered_links
    ADD CONSTRAINT discovered_links_tenant_id_local_ip_local_port_remote_chassi_key
    UNIQUE (tenant_id, local_ip, local_port, remote_chassis_id, remote_port_id);
