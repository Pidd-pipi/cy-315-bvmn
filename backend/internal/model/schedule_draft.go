package model

import "gorm.io/gorm"

// ScheduleDraft is a named, immutable snapshot saved from the current timetable.
// A draft can be published at most once; after publishing it is kept for audit
// and cannot replace the current timetable again.
type ScheduleDraft struct {
	gorm.Model
	Name        string `gorm:"size:128;not null;uniqueIndex:idx_schedule_drafts_name" json:"name"`
	Status      string `gorm:"size:32;not null;index;default:draft" json:"status"`
	ItemCount   int    `gorm:"not null;default:0" json:"item_count"`
	CreatedBy   string `gorm:"size:64;not null;default:''" json:"created_by"`
	PublishedBy string `gorm:"size:64;not null;default:''" json:"published_by"`
	PublishedAt *int64 `json:"published_at"`
}

// TableName explicitly names the draft table.
func (ScheduleDraft) TableName() string { return "schedule_drafts" }

// ScheduleDraftItem is one timetable entry captured inside a draft snapshot.
// Rows are immutable once the draft is created.
type ScheduleDraftItem struct {
	gorm.Model
	DraftID     uint `gorm:"not null;index:idx_schedule_draft_items_draft_id" json:"draft_id"`
	Week        uint `gorm:"not null" json:"week"`
	DayOfWeek   int  `gorm:"not null" json:"day_of_week"`
	TimeSlotID  uint `gorm:"not null" json:"time_slot_id"`
	ClassroomID uint `gorm:"not null" json:"classroom_id"`
	TeacherID   uint `gorm:"not null" json:"teacher_id"`
	ClassID     uint `gorm:"not null" json:"class_id"`
	CourseID    uint `gorm:"not null" json:"course_id"`
}

// TableName explicitly names the draft item table.
func (ScheduleDraftItem) TableName() string { return "schedule_draft_items" }

// SchedulePublishRecord is the audit record of a successful draft publication.
// At most one record can exist for a given draft, enforcing the
// "publish the same draft only once" rule even under concurrent requests.
type SchedulePublishRecord struct {
	gorm.Model
	DraftID     uint   `gorm:"not null;uniqueIndex" json:"draft_id"`
	DraftName   string `gorm:"size:128;not null" json:"draft_name"`
	PublishedBy string `gorm:"size:64;not null" json:"published_by"`
	PublishedAt int64  `gorm:"not null" json:"published_at"`
	ItemCount   int    `gorm:"not null;default:0" json:"item_count"`
}

// TableName explicitly names the publish record table.
func (SchedulePublishRecord) TableName() string { return "schedule_publish_records" }
