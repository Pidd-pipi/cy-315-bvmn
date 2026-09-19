-- 课表草稿与发布审计 (SQLite)
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schedule_drafts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    item_count INTEGER NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL DEFAULT '',
    published_by TEXT NOT NULL DEFAULT '',
    published_at INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_drafts_name ON schedule_drafts(name);
CREATE INDEX IF NOT EXISTS idx_schedule_drafts_status ON schedule_drafts(status);

CREATE TABLE IF NOT EXISTS schedule_draft_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    draft_id INTEGER NOT NULL,
    week INTEGER NOT NULL,
    day_of_week INTEGER NOT NULL,
    time_slot_id INTEGER NOT NULL,
    classroom_id INTEGER NOT NULL,
    teacher_id INTEGER NOT NULL,
    class_id INTEGER NOT NULL,
    course_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schedule_draft_items_draft_id ON schedule_draft_items(draft_id);

CREATE TABLE IF NOT EXISTS schedule_publish_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    draft_id INTEGER NOT NULL,
    draft_name TEXT NOT NULL,
    published_by TEXT NOT NULL,
    published_at INTEGER NOT NULL,
    item_count INTEGER NOT NULL DEFAULT 0
);
-- At most one successful publish per draft, enforced at the storage layer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_publish_records_draft_id ON schedule_publish_records(draft_id);
