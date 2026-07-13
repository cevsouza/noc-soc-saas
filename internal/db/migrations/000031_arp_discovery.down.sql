-- 000031_arp_discovery.down.sql
ALTER TABLE discovered_devices DROP COLUMN IF EXISTS source;
ALTER TABLE discovered_devices DROP COLUMN IF EXISTS mac;
