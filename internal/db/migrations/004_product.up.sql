CREATE TABLE monitors (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name             text NOT NULL,
  url              text NOT NULL,
  method           text NOT NULL DEFAULT 'GET',
  expected_status  int  NOT NULL DEFAULT 200,
  interval_seconds int  NOT NULL DEFAULT 60 CHECK (interval_seconds >= 30),
  timeout_ms       int  NOT NULL DEFAULT 10000,
  is_paused        boolean NOT NULL DEFAULT false,
  next_check_at    timestamptz NOT NULL DEFAULT now(),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX monitors_org_idx ON monitors(org_id);
CREATE INDEX monitors_due_idx ON monitors(next_check_at) WHERE is_paused = false;

CREATE TABLE check_results (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  monitor_id    uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status        text NOT NULL CHECK (status IN ('up','down')),
  status_code   int,
  latency_ms    int,
  error         text,
  checked_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX check_results_monitor_time_idx ON check_results(monitor_id, checked_at DESC);
CREATE INDEX check_results_org_idx          ON check_results(org_id);

CREATE TABLE incidents (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  monitor_id   uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  title        text NOT NULL,
  started_at   timestamptz NOT NULL DEFAULT now(),
  resolved_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX incidents_org_idx     ON incidents(org_id);
CREATE INDEX incidents_monitor_idx ON incidents(monitor_id);
CREATE UNIQUE INDEX incidents_one_open_per_monitor
  ON incidents(monitor_id) WHERE status = 'open';

CREATE TABLE incident_updates (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  incident_id  uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  message      text NOT NULL,
  status       text NOT NULL,
  author_id    uuid REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX incident_updates_incident_idx ON incident_updates(incident_id, created_at);
CREATE INDEX incident_updates_org_idx      ON incident_updates(org_id);

CREATE TABLE status_pages (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  slug         citext UNIQUE NOT NULL,
  title        text NOT NULL,
  is_public    boolean NOT NULL DEFAULT false,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX status_pages_org_idx ON status_pages(org_id);

CREATE TABLE status_page_monitors (
  status_page_id uuid NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  monitor_id     uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  org_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  PRIMARY KEY (status_page_id, monitor_id)
);
CREATE INDEX status_page_monitors_org_idx ON status_page_monitors(org_id);
