-- Restore the blanket DML grant from migration 007.
GRANT UPDATE, DELETE ON global_audit_logs TO app_user;
