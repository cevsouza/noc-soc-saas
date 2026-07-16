-- 000040_user_default_console.up.sql
--
-- Per-user default console preference (B9): a NOC team member can set their landing console to 'noc'
-- so they arrive straight at /noc after login (SOC → /soc). This is a convenience/view preference,
-- NOT an access restriction — every user still sees all domains via the dedicated routes. 'all' is
-- the neutral default (lands on the unified cockpit /). Column on the global users table (no RLS).
ALTER TABLE users ADD COLUMN IF NOT EXISTS default_console VARCHAR(10) NOT NULL DEFAULT 'all';
