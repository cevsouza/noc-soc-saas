-- 000030_asset_aliases.down.sql
ALTER TABLE assets DROP COLUMN IF EXISTS aliases;
