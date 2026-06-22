-- Incidents can be opened two ways: by a human (incidents.Service.Create) or
-- automatically by the worker's failure-streak engine. Both share the partial
-- unique index incidents_one_open_per_monitor, so at most one is ever open per
-- monitor — but until now the rows were indistinguishable, which let the
-- worker's auto-resolve close a human-authored incident on monitor recovery.
--
-- `source` records who opened the incident so the worker can scope its
-- auto-resolve to its OWN incidents and never silently close a manual one.
-- Existing rows default to 'manual' (the only pre-worker origin).
ALTER TABLE incidents
  ADD COLUMN source text NOT NULL DEFAULT 'manual'
  CHECK (source IN ('manual', 'worker'));
