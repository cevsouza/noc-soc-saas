-- 000009_enterprise_features.up.sql

-- 1. Add white-label customization columns to tenants table
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS logo_url VARCHAR(512) DEFAULT NULL;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS primary_color VARCHAR(50) DEFAULT NULL;

-- 2. Add SOAR auto-trigger option to tenant_runbooks table
ALTER TABLE tenant_runbooks ADD COLUMN IF NOT EXISTS auto_trigger BOOLEAN DEFAULT FALSE;

-- 3. Add ITSM and security compliance columns to alerts table
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS itsm_ticket_ref VARCHAR(50) DEFAULT NULL;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS mitre_tactics VARCHAR(255) DEFAULT NULL;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ueba_anomalous BOOLEAN DEFAULT FALSE;
