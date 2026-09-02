PRAGMA foreign_keys = ON;

CREATE TABLE id_owners (
    id               TEXT PRIMARY KEY CHECK (id <> ''),
    kind             TEXT NOT NULL CHECK (kind IN ('issue', 'memory')),
    creation_replica TEXT NOT NULL CHECK (creation_replica <> ''),
    UNIQUE (id, kind)
) STRICT;

CREATE TABLE projects (
    project_key        TEXT PRIMARY KEY CHECK (project_key <> ''),
    slug               TEXT NOT NULL UNIQUE CHECK (slug <> ''),
    archived_at        TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    creation_replica   TEXT NOT NULL CHECK (creation_replica <> ''),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica   TEXT NOT NULL CHECK (revision_replica <> '')
) STRICT;

CREATE TABLE repository_locators (
    project_key        TEXT NOT NULL REFERENCES projects(project_key),
    locator            TEXT NOT NULL CHECK (locator <> ''),
    tombstoned         INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica   TEXT NOT NULL CHECK (revision_replica <> ''),
    PRIMARY KEY (project_key, locator)
) STRICT;

CREATE INDEX idx_repository_locators_live_locator
    ON repository_locators(locator, project_key)
    WHERE tombstoned = 0;

CREATE TABLE workspace_bindings (
    path        TEXT PRIMARY KEY CHECK (path <> ''),
    project_key TEXT NOT NULL REFERENCES projects(project_key)
) STRICT;

CREATE INDEX idx_workspace_bindings_project
    ON workspace_bindings(project_key);

CREATE TABLE issues (
    id                  TEXT PRIMARY KEY CHECK (id <> ''),
    owner_kind          TEXT NOT NULL DEFAULT 'issue' CHECK (owner_kind = 'issue'),
    project_key         TEXT NOT NULL REFERENCES projects(project_key),
    title               TEXT NOT NULL CHECK (title <> ''),
    description         TEXT NOT NULL DEFAULT '',
    issue_type          TEXT NOT NULL CHECK (issue_type IN ('task', 'bug', 'feature', 'epic', 'chore', 'research', 'decision')),
    status              TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'closed')),
    priority            INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 4),
    assignee            TEXT,
    close_reason        TEXT,
    deferred_until      TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    closed_at           TEXT,
    started_at          TEXT,
    tombstoned          INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica    TEXT NOT NULL CHECK (revision_replica <> ''),
    FOREIGN KEY (id, owner_kind) REFERENCES id_owners(id, kind)
) STRICT;

CREATE INDEX idx_issues_project_queue
    ON issues(project_key, tombstoned, status, priority, created_at DESC, id);
CREATE INDEX idx_issues_status
    ON issues(tombstoned, status);

CREATE TABLE issue_parents (
    child_id            TEXT PRIMARY KEY REFERENCES issues(id),
    parent_id           TEXT NOT NULL REFERENCES issues(id),
    created_at          TEXT NOT NULL,
    tombstoned          INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica    TEXT NOT NULL CHECK (revision_replica <> ''),
    CHECK (child_id <> parent_id)
) STRICT;

CREATE INDEX idx_issue_parents_live_parent
    ON issue_parents(parent_id, child_id)
    WHERE tombstoned = 0;

CREATE TABLE dependencies (
    from_id             TEXT NOT NULL REFERENCES issues(id),
    to_id               TEXT NOT NULL REFERENCES issues(id),
    dep_type            TEXT NOT NULL CHECK (dep_type IN ('blocks', 'related', 'discovered-from')),
    created_at          TEXT NOT NULL,
    tombstoned          INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica    TEXT NOT NULL CHECK (revision_replica <> ''),
    PRIMARY KEY (from_id, to_id, dep_type),
    CHECK (from_id <> to_id)
) STRICT;

CREATE INDEX idx_dependencies_live_to
    ON dependencies(to_id, dep_type, from_id)
    WHERE tombstoned = 0;
CREATE INDEX idx_dependencies_live_type_from
    ON dependencies(dep_type, from_id, to_id)
    WHERE tombstoned = 0;

CREATE TABLE labels (
    issue_id            TEXT NOT NULL REFERENCES issues(id),
    label               TEXT NOT NULL CHECK (label <> ''),
    tombstoned          INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica    TEXT NOT NULL CHECK (revision_replica <> ''),
    PRIMARY KEY (issue_id, label)
) STRICT;

CREATE INDEX idx_labels_live_label
    ON labels(label, issue_id)
    WHERE tombstoned = 0;

CREATE TABLE comments (
    id               TEXT PRIMARY KEY CHECK (id <> ''),
    issue_id         TEXT NOT NULL REFERENCES issues(id),
    author           TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    creation_replica TEXT NOT NULL CHECK (creation_replica <> '')
) STRICT;

CREATE INDEX idx_comments_issue
    ON comments(issue_id, created_at, id);

CREATE TABLE memories (
    id                  TEXT PRIMARY KEY CHECK (id <> ''),
    owner_kind          TEXT NOT NULL DEFAULT 'memory' CHECK (owner_kind = 'memory'),
    project_key         TEXT NOT NULL REFERENCES projects(project_key),
    title               TEXT NOT NULL CHECK (title <> ''),
    body                TEXT NOT NULL CHECK (body <> ''),
    provenance          TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    superseded_by       TEXT,
    tombstoned          INTEGER NOT NULL DEFAULT 0 CHECK (tombstoned IN (0, 1)),
    revision_generation INTEGER NOT NULL CHECK (revision_generation >= 1),
    revision_replica    TEXT NOT NULL CHECK (revision_replica <> ''),
    UNIQUE (id, project_key),
    CHECK (superseded_by IS NULL OR superseded_by <> id),
    FOREIGN KEY (id, owner_kind) REFERENCES id_owners(id, kind),
    FOREIGN KEY (superseded_by, project_key) REFERENCES memories(id, project_key)
) STRICT;

CREATE INDEX idx_memories_live
    ON memories(project_key, updated_at DESC, id)
    WHERE tombstoned = 0 AND superseded_by IS NULL;
CREATE INDEX idx_memories_superseded_by
    ON memories(superseded_by)
    WHERE superseded_by IS NOT NULL;

CREATE TABLE store_state (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    write_seq          INTEGER NOT NULL DEFAULT 0 CHECK (write_seq >= 0),
    exported_write_seq INTEGER NOT NULL DEFAULT 0 CHECK (exported_write_seq BETWEEN 0 AND write_seq),
    last_export_head   TEXT
) STRICT;

INSERT INTO store_state(id) VALUES (1);

CREATE TABLE replica_heads (
    replica_key  TEXT PRIMARY KEY CHECK (replica_key <> ''),
    snapshot_head TEXT NOT NULL CHECK (snapshot_head <> '')
) STRICT;

CREATE TABLE replica_successions (
    old_replica_key TEXT PRIMARY KEY CHECK (old_replica_key <> ''),
    new_replica_key TEXT NOT NULL UNIQUE CHECK (new_replica_key <> ''),
    rekeyed_at      TEXT NOT NULL,
    CHECK (old_replica_key <> new_replica_key)
) STRICT;

CREATE VIRTUAL TABLE issues_fts USING fts5(
    title,
    description,
    content = 'issues',
    content_rowid = 'rowid'
);
CREATE VIRTUAL TABLE comments_fts USING fts5(
    body,
    content = 'comments',
    content_rowid = 'rowid'
);
CREATE VIRTUAL TABLE memories_fts USING fts5(
    title,
    body,
    content = 'memories',
    content_rowid = 'rowid'
);

CREATE TRIGGER issues_fts_ai AFTER INSERT ON issues BEGIN
    INSERT INTO issues_fts(rowid, title, description)
    VALUES (new.rowid, new.title, new.description);
END;
CREATE TRIGGER issues_fts_ad AFTER DELETE ON issues BEGIN
    INSERT INTO issues_fts(issues_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, old.description);
END;
CREATE TRIGGER issues_fts_au AFTER UPDATE OF title, description ON issues BEGIN
    INSERT INTO issues_fts(issues_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, old.description);
    INSERT INTO issues_fts(rowid, title, description)
    VALUES (new.rowid, new.title, new.description);
END;

CREATE TRIGGER comments_fts_ai AFTER INSERT ON comments BEGIN
    INSERT INTO comments_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TRIGGER comments_fts_ad AFTER DELETE ON comments BEGIN
    INSERT INTO comments_fts(comments_fts, rowid, body)
    VALUES ('delete', old.rowid, old.body);
END;
CREATE TRIGGER comments_fts_au AFTER UPDATE OF body ON comments BEGIN
    INSERT INTO comments_fts(comments_fts, rowid, body)
    VALUES ('delete', old.rowid, old.body);
    INSERT INTO comments_fts(rowid, body) VALUES (new.rowid, new.body);
END;

CREATE TRIGGER memories_fts_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;
CREATE TRIGGER memories_fts_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
END;
CREATE TRIGGER memories_fts_au AFTER UPDATE OF title, body ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
    INSERT INTO memories_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END;

PRAGMA user_version = 7;
