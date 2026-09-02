CREATE TABLE issues (
  id                  TEXT PRIMARY KEY,
  project_id          INTEGER NOT NULL REFERENCES projects(id),
  title               TEXT NOT NULL,
  description         TEXT NOT NULL DEFAULT '',
  issue_type          TEXT NOT NULL
    CHECK (issue_type IN ('task','bug','feature','epic','chore','research','decision')),
  status              TEXT NOT NULL
    CHECK (status IN ('open','in_progress','blocked','closed','deleted')),
  priority            INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 4),
  assignee            TEXT,
  close_reason        TEXT,
  deferred_until      TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  closed_at           TEXT
, started_at TEXT, metadata   TEXT);
CREATE INDEX idx_issues_project_status ON issues(project_id, status);
CREATE INDEX idx_issues_status         ON issues(status);
CREATE TABLE dependencies (
  from_id    TEXT NOT NULL REFERENCES issues(id),
  to_id      TEXT NOT NULL REFERENCES issues(id),
  dep_type   TEXT NOT NULL
    CHECK (dep_type IN ('blocks','parent-child','related','discovered-from')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (from_id, to_id, dep_type),
  CHECK (from_id <> to_id)
);
CREATE INDEX idx_deps_to        ON dependencies(to_id, dep_type);
CREATE INDEX idx_deps_type_from ON dependencies(dep_type, from_id);
CREATE TABLE labels (
  issue_id TEXT NOT NULL REFERENCES issues(id),
  label    TEXT NOT NULL,
  PRIMARY KEY (issue_id, label)
);
CREATE TABLE comments (
  id         TEXT PRIMARY KEY,
  issue_id   TEXT NOT NULL REFERENCES issues(id),
  author     TEXT NOT NULL DEFAULT '',
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_comments_issue ON comments(issue_id);
CREATE TABLE memories (
  id             TEXT PRIMARY KEY,
  project_id     INTEGER NULL REFERENCES projects(id),
  kind           TEXT NOT NULL CHECK (kind IN (
                   'root-cause','convention','gotcha','decision','reference',
                   'lesson','research','correction','pattern')),
  title          TEXT NOT NULL,
  body           TEXT NOT NULL,
  ambient        INTEGER NOT NULL DEFAULT 0 CHECK (ambient IN (0,1)),
  salience       INTEGER NOT NULL DEFAULT 3 CHECK (salience BETWEEN 1 AND 5),
  source_project TEXT NOT NULL DEFAULT '',
  source         TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  last_recalled_at TEXT,
  recall_count   INTEGER NOT NULL DEFAULT 0,
  deleted_at     TEXT,
  superseded_by  TEXT REFERENCES memories(id),
  CHECK (superseded_by IS NULL OR superseded_by <> id)
);
CREATE INDEX idx_memories_live ON memories(project_id, kind)
  WHERE deleted_at IS NULL AND superseded_by IS NULL;
CREATE INDEX idx_memories_prime ON memories(ambient DESC, salience DESC, updated_at DESC)
  WHERE deleted_at IS NULL AND superseded_by IS NULL;
CREATE VIRTUAL TABLE memories_fts USING fts5(
  title, body, content='memories', content_rowid='rowid')
/* memories_fts(title,body) */;
CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
  INSERT INTO memories_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
END;
CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
  INSERT INTO memories_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
CREATE TABLE sync_state (
  id                 INTEGER PRIMARY KEY CHECK (id = 1),
  -- When the OLDEST currently-unexported write happened, or NULL if the
  -- mirror is current. Not "the most recent write": see markDirtyTx.
  dirty_at           TEXT,
  last_export_at     TEXT,
  last_commit_at     TEXT,
  last_commit_sha    TEXT,
  last_dolt_warn_at  TEXT,
  -- Duration of the last export, for doctor's scaling report (spec §8.2).
  last_export_ms     INTEGER NOT NULL DEFAULT 0,
  -- Whether the last sync ran WITHOUT the advisory lock, because flock was
  -- unavailable on this filesystem rather than held (spec §6.3). Doctor
  -- reports it so a permanently lock-less filesystem is visible.
  last_sync_unlocked INTEGER NOT NULL DEFAULT 0
    CHECK (last_sync_unlocked IN (0,1))
);
CREATE VIRTUAL TABLE issues_fts USING fts5(
  title, description, content='issues', content_rowid='rowid')
/* issues_fts(title,description) */;
CREATE TRIGGER issues_ai AFTER INSERT ON issues BEGIN
  INSERT INTO issues_fts(rowid, title, description)
    VALUES (new.rowid, new.title, new.description);
END;
CREATE TRIGGER issues_ad AFTER DELETE ON issues BEGIN
  INSERT INTO issues_fts(issues_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, old.description);
END;
CREATE TRIGGER issues_au AFTER UPDATE ON issues BEGIN
  INSERT INTO issues_fts(issues_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, old.description);
  INSERT INTO issues_fts(rowid, title, description)
    VALUES (new.rowid, new.title, new.description);
END;
CREATE VIRTUAL TABLE comments_fts USING fts5(
  body, content='comments', content_rowid='rowid')
/* comments_fts(body) */;
CREATE TRIGGER comments_ai AFTER INSERT ON comments BEGIN
  INSERT INTO comments_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TRIGGER comments_ad AFTER DELETE ON comments BEGIN
  INSERT INTO comments_fts(comments_fts, rowid, body)
    VALUES ('delete', old.rowid, old.body);
END;
CREATE TRIGGER comments_au AFTER UPDATE ON comments BEGIN
  INSERT INTO comments_fts(comments_fts, rowid, body)
    VALUES ('delete', old.rowid, old.body);
  INSERT INTO comments_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TABLE "projects" (
  id         INTEGER PRIMARY KEY,
  slug       TEXT NOT NULL UNIQUE,
  repo_path  TEXT UNIQUE,
  remote_url TEXT,
  archived_at TEXT,
  created_at TEXT NOT NULL
);
PRAGMA user_version = 6;
