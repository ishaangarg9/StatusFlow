-- Actually invoke retention. prune_audit_logs (014) existed but nothing called
-- it, so the audit tables would grow forever. Where pg_cron is available
-- (Supabase / most managed Postgres), schedule the daily prune on the
-- privileged connection that owns the function. Where pg_cron is absent (e.g.
-- local docker), this is a no-op — plpgsql resolves cron.schedule only inside
-- the taken branch — and the operator schedules prune_audit_logs(...) externally.
--
-- The retention window (365 days) is the operator's policy; adjust the interval
-- or reschedule the job to change it. cron.schedule upserts by job name, so
-- re-running this migration is idempotent.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
    PERFORM cron.schedule(
      'statusflow-prune-audit-logs',
      '17 3 * * *',
      $cron$SELECT prune_audit_logs(interval '365 days')$cron$
    );
  END IF;
END $$;
