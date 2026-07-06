DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron')
     AND EXISTS (SELECT 1 FROM cron.job WHERE jobname = 'statusflow-prune-check-results') THEN
    PERFORM cron.unschedule('statusflow-prune-check-results');
  END IF;
END $$;

DROP FUNCTION IF EXISTS prune_check_results(interval);
