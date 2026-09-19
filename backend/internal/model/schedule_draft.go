package model

import (
	"time"

	"gorm.io/gorm"
)

// ScheduleDraft is a named snapshot of the full timetable that can be
// reviewed and later published to replace the active schedule.
type ScheduleDraft struct {
	gorm.Model
	Name        string     `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Description string     `gorm:"size:512" json:"description"`
	Status      string     `gorm:"size:16;not null;default:draft;index" json:"status"`
	EntryCount  int        `gorm:"not null;default:0" json:"entry_count"`
	PublishedBy string     `gorm:"size:128" json:"published_by"`
	PublishedAt *time.Time `json:"published_at"`
}

// TableName explicitly names the table to avoid GORM's default pluralization.
func (ScheduleDraft) TableName() string { return "schedule_drafts" }

// ScheduleDraftEntry is one lesson entry inside a schedule draft snapshot.
type ScheduleDraftEntry struct {
	gorm.Model
	DraftID     uint `gorm:"index;not null" json:"draft_id"`
	Week        uint `gorm:"not null" json:"week"`
	DayOfWeek   int  `gorm:"not null" json:"day_of_week"`
	TimeSlotID  uint `gorm:"not null" json:"time_slot_id"`
	ClassroomID uint `gorm:"not null" json:"classroom_id"`
	TeacherID   uint `gorm:"not null" json:"teacher_id"`
	ClassID     uint `gorm:"not null" json:"class_id"`
	CourseID    uint `gorm:"not null" json:"course_id"`
}

// TableName explicitly names the table to avoid GORM's default pluralization.
func (ScheduleDraftEntry) TableName() string { return "schedule_draft_entries" }
