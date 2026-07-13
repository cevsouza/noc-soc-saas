-- 000031_arp_discovery.up.sql
--
-- ARP discovery (topology slice T5). The SNMP sweep only finds SNMP-speaking gear; endpoints, printers
-- and IoT devices are invisible. Now the agent also walks each SNMP device's ARP cache (ipNetToMedia)
-- and reports the IP↔MAC entries it finds. Those non-SNMP hosts are recorded in the same inventory with
-- source='arp' and a device_type of 'endpoint', carrying their MAC. A host that later answers SNMP is
-- upgraded in place to source='snmp' with full identity (the SNMP upsert wins).
--
-- source distinguishes how a device was found; mac stores the ARP-learned hardware address.

ALTER TABLE discovered_devices ADD COLUMN IF NOT EXISTS mac VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE discovered_devices ADD COLUMN IF NOT EXISTS source VARCHAR(8) NOT NULL DEFAULT 'snmp';
