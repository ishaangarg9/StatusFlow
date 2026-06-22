DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron')
     AND EXISTS (SELECT 1 FROM cron.job WHERE jobname = 'statusflow-prune-audit-logs') THEN
    PERFORM cron.unschedule('statusflow-prune-audit-logs');
  END IF;
END $$;
