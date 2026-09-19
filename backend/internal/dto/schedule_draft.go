package dto

// CreateScheduleDraftRequest saves the current timetable as a named draft.
type CreateScheduleDraftRequest struct {
	Name      string `json:"name" binding:"required,min=1,max=128"`
	CreatedBy string `json:"created_by" binding:"omitempty,max=64"`
}

// ScheduleDraftSummary is one row of the paginated draft list.
type ScheduleDraftSummary struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	ItemCount   int    `json:"item_count"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	PublishedBy string `json:"published_by"`
	PublishedAt string `json:"published_at"`
}

// ScheduleDraftItemResponse is one entry of a draft snapshot.
type ScheduleDraftItemResponse struct {
	ID            uint   `json:"id"`
	Week          uint   `json:"week"`
	DayOfWeek     int    `json:"day_of_week"`
	TimeSlotID    uint   `json:"time_slot_id"`
	TimeSlotCode  string `json:"time_slot_code"`
	TimeSlotName  string `json:"time_slot_name"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time"`
	ClassroomID   uint   `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	TeacherID     uint   `json:"teacher_id"`
	TeacherName   string `json:"teacher_name"`
	ClassID       uint   `json:"class_id"`
	ClassName     string `json:"class_name"`
	CourseID      uint   `json:"course_id"`
	CourseName    string `json:"course_name"`
}

// ScheduleDraftDetailResponse is the full draft including its snapshot.
type ScheduleDraftDetailResponse struct {
	ScheduleDraftSummary
	Items []ScheduleDraftItemResponse `json:"items"`
}

// PublishScheduleDraftRequest publishes a saved draft.
type PublishScheduleDraftRequest struct {
	PublishedBy string `json:"published_by" binding:"required,min=1,max=64"`
}

// PublishScheduleDraftResponse is the result of a successful publication.
type PublishScheduleDraftResponse struct {
	PublishID   uint               `json:"publish_id"`
	DraftID     uint               `json:"draft_id"`
	DraftName   string             `json:"draft_name"`
	Status      string             `json:"status"`
	PublishedBy string             `json:"published_by"`
	PublishedAt string             `json:"published_at"`
	ItemCount   int                `json:"item_count"`
	Schedules   []ScheduleResponse `json:"schedules"`
}

// SchedulePublishRecordResponse is one publish audit entry.
type SchedulePublishRecordResponse struct {
	ID          uint   `json:"id"`
	DraftID     uint   `json:"draft_id"`
	DraftName   string `json:"draft_name"`
	PublishedBy string `json:"published_by"`
	PublishedAt string `json:"published_at"`
	ItemCount   int    `json:"item_count"`
}
