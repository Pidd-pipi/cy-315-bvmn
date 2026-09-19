package dto

// SaveDraftRequest is the payload for saving the current timetable as a draft.
type SaveDraftRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=128"`
	Description string `json:"description" binding:"omitempty,max=512"`
}

// DraftResponse is the summary representation of a schedule draft.
type DraftResponse struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	EntryCount  int     `json:"entry_count"`
	PublishedBy string  `json:"published_by"`
	PublishedAt *string `json:"published_at"`
	CreatedAt   string  `json:"created_at"`
}

// DraftDetailResponse is a draft together with its snapshot entries.
type DraftDetailResponse struct {
	DraftResponse
	Entries []ScheduleResponse `json:"entries"`
}

// PublishDraftRequest is the payload for publishing a draft.
type PublishDraftRequest struct {
	PublishedBy string `json:"published_by" binding:"required,min=1,max=128"`
}

// PublishDraftResponse is the result of a successful publish.
type PublishDraftResponse struct {
	DraftID     uint   `json:"draft_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	PublishedBy string `json:"published_by"`
	PublishedAt string `json:"published_at"`
	Replaced    int    `json:"replaced"`
}
