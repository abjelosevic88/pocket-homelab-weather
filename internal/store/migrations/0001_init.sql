CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);



-- ---------- Pocket Weather: everything below is per person ----------

-- A place whose weather the person follows: home, work, the cottage, a trip's
-- city. One is home (the default everywhere: the morning message, Pocket Day,
-- Pocket AI). "here" places follow the person through Pocket Movement.
CREATE TABLE locations (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,                         -- "Sarajevo", "The cottage"
  region       TEXT NOT NULL DEFAULT '',              -- "Federation of B&H"
  country      TEXT NOT NULL DEFAULT '',
  country_code TEXT NOT NULL DEFAULT '',              -- BA
  lat          REAL NOT NULL,
  lon          REAL NOT NULL,
  elevation    REAL,
  timezone     TEXT NOT NULL DEFAULT '',              -- Europe/Sarajevo
  is_home      INTEGER NOT NULL DEFAULT 0,
  warn         INTEGER NOT NULL DEFAULT 1,            -- warnings and the diary for this place
  sort         INTEGER NOT NULL DEFAULT 0,
  note         TEXT NOT NULL DEFAULT '',
  source       TEXT NOT NULL DEFAULT '',              -- '' by hand | here | import | <app slug>
  source_ref   TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX locations_user ON locations(user_id, sort, name);
CREATE UNIQUE INDEX locations_source ON locations(user_id, source, source_ref) WHERE source_ref <> '';

-- The weather diary: what a day was like at a place, kept after the forecast
-- has moved on (written each night from the forecast's "yesterday").
CREATE TABLE days (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  location_id  INTEGER NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
  date         TEXT NOT NULL,                         -- YYYY-MM-DD, local to the place
  code         INTEGER NOT NULL DEFAULT -1,           -- WMO weather code
  t_min        REAL,
  t_max        REAL,
  precip       REAL,                                  -- mm
  snow         REAL,                                  -- cm
  wind         REAL,                                  -- km/h, strongest
  gust         REAL,
  uv           REAL,
  sunshine_h   REAL,
  aqi          REAL,                                  -- worst European AQI of the day
  pollen       TEXT NOT NULL DEFAULT '{}',            -- {"grass": 31, …} highest grains/m³
  note         TEXT NOT NULL DEFAULT '',              -- the person's own words
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, location_id, date)
);
CREATE INDEX days_date ON days(user_id, date);

-- Warnings Weather works out from the forecast: one per kind, place and day,
-- so a warning is told once and again only when it gets worse.
CREATE TABLE warnings (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  location_id  INTEGER NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL,                         -- frost | ice | heat | wind | rain | snow | storm | uv | pollen | air
  level        TEXT NOT NULL DEFAULT 'advisory',      -- advisory ("heads-up") | warning
  day          TEXT NOT NULL,                         -- YYYY-MM-DD it is about
  starts_at    TEXT NOT NULL DEFAULT '',              -- local hour it starts, 2026-11-06T03:00
  ends_at      TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL,
  detail       TEXT NOT NULL DEFAULT '',
  value        REAL,
  notified     TEXT NOT NULL DEFAULT '',              -- the level last pushed ('' = not yet)
  gone         INTEGER NOT NULL DEFAULT 0,            -- the forecast no longer shows it
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX warnings_key ON warnings(user_id, location_id, kind, day);
CREATE INDEX warnings_day ON warnings(user_id, day);

-- What the person did with a piece of advice ("time for winter tyres"):
-- done, hidden, or made into a task in Pocket Tasks.
CREATE TABLE advice_marks (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key          TEXT NOT NULL,                         -- tyres:winter:2026, plants:frost:2026-11-06
  state        TEXT NOT NULL,                         -- done | hidden | task
  ref          TEXT NOT NULL DEFAULT '',              -- the task's id
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, key)
);
