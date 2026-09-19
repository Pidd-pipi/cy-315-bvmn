-- 课表草稿与发布闭环 schedule drafts & publish (SQLite)
CREATE TABLE IF NOT EXISTS schedule_drafts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    entry_count INTEGER NOT NULL DEFAULT 0,
    published_by TEXT,
    published_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_schedule_drafts_status ON schedule_drafts(status);

CREATE TABLE IF NOT EXISTS schedule_draft_entries (
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
CREATE INDEX IF NOT EXISTS idx_schedule_draft_entries_draft ON schedule_draft_entries(draft_id);
