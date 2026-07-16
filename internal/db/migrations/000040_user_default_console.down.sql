-- 000040_user_default_console.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS default_console;
