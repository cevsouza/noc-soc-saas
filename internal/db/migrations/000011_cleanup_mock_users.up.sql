-- 000011_cleanup_mock_users.up.sql
-- One-time database cleanup of seeded users to prevent recurring script deletion issues.

-- 1. Remove bindings from tenant_users first due to foreign key constraints
DELETE FROM tenant_users WHERE user_id IN (
    SELECT id FROM users WHERE email IN (
        'admin@itfacil.com.br',
        'cevsouza@hotmail.com',
        'cadu.souza@itfacilservicos.com.br',
        'felipe.gomes@itfacilservicos.com.br'
    )
);

-- 2. Delete users from main users table
DELETE FROM users WHERE email IN (
    'admin@itfacil.com.br',
    'cevsouza@hotmail.com',
    'cadu.souza@itfacilservicos.com.br',
    'felipe.gomes@itfacilservicos.com.br'
);
