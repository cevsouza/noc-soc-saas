-- 000005_user_auth.up.sql

-- 1. Alter users table to support password auth, verification, and roles
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_verified BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS verification_token VARCHAR(255) DEFAULT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS global_role VARCHAR(50) DEFAULT 'operator';

-- 2. Seed the standard tenants to replace mock data seamlessly
INSERT INTO tenants (id, name, slug, status)
VALUES 
  ('e1b7c123-1234-4321-abcd-123456789abc', 'Telco Global Corp (Tenant A)', 'telco-global', 'active'),
  ('fa2b2345-5678-8765-dcba-987654321fed', 'Quantum Cloud Inc (Tenant B)', 'quantum-cloud', 'active')
ON CONFLICT (id) DO NOTHING;
