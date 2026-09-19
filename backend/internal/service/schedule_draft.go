package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

// DraftConflictError reports that a draft failed the pre-publish conflict
// check. It carries the detected conflicts so the handler can return them to
// the caller, and matches ErrDraftHasConflicts via errors.Is.
type DraftConflictError struct {
	Conflicts []dto.ConflictResponse
}

func (e *DraftConflictError) Error() string { return ErrDraftHasConflicts.Error() }

// Is lets errors.Is(err, ErrDraftHasConflicts) match this error type.
func (e *DraftConflictError) Is(target error) bool { return target == ErrDraftHasConflicts }

// SaveDraft snapshots the current timetable into a new named draft.
func (s *scheduleService) SaveDraft(ctx context.Context, req *dto.SaveDraftRequest) (*dto.DraftResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("save draft: %w: name must not be blank", ErrInvalid)
	}
	items, err := s.schedules.List(ctx, repository.ScheduleFilter{})
	if err != nil {
		return nil, fmt.Errorf("save draft: load current schedules: %w", err)
	}
	draft := &model.ScheduleDraft{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Status:      constants.DraftStatusDraft,
		EntryCount:  len(items),
	}
	entries := make([]model.ScheduleDraftEntry, 0, len(items))
	for i := range items {
		entries = append(entries, scheduleToDraftEntry(items[i]))
	}
	if err := s.drafts.CreateWithEntries(ctx, draft, entries); err != nil {
		if errors.Is(err, repository.ErrConstraint) {
			return nil, ErrDraftNameExists
		}
		return nil, fmt.Errorf("save draft: %w", err)
	}
	resp := draftResponse(draft)
	return &resp, nil
}

// ListDrafts returns a page of drafts without their entries.
func (s *scheduleService) ListDrafts(ctx context.Context, page, pageSize int) ([]dto.DraftResponse, int64, error) {
	items, total, err := s.drafts.List(ctx, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list drafts: %w", err)
	}
	out := make([]dto.DraftResponse, 0, len(items))
	for i := range items {
		out = append(out, draftResponse(&items[i]))
	}
	return out, total, nil
}

// GetDraft returns one draft with its snapshot entries enriched like the live timetable.
func (s *scheduleService) GetDraft(ctx context.Context, id uint) (*dto.DraftDetailResponse, error) {
	draft, err := s.drafts.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get draft: %w", err)
	}
	entries, err := s.drafts.ListEntries(ctx, draft.ID)
	if err != nil {
		return nil, fmt.Errorf("get draft entries: %w", err)
	}
	enriched, err := s.enrichSchedules(ctx, draftEntriesToSchedules(entries))
	if err != nil {
		return nil, fmt.Errorf("get draft: %w", err)
	}
	detail := dto.DraftDetailResponse{DraftResponse: draftResponse(draft), Entries: enriched}
	return &detail, nil
}

// PublishDraft replaces the live timetable with the draft content in a single
// transaction after a conflict check. Exactly one concurrent publish of the
// same draft can win; every later attempt fails with ErrDraftAlreadyPublished.
func (s *scheduleService) PublishDraft(ctx context.Context, id uint, req *dto.PublishDraftRequest) (*dto.PublishDraftResponse, error) {
	publishedBy := strings.TrimSpace(req.PublishedBy)
	if publishedBy == "" {
		return nil, fmt.Errorf("publish draft: %w: published_by must not be blank", ErrInvalid)
	}

	// Serialize publishes in-process so a concurrent attempt observes a clean
	// "already published" outcome instead of a database lock error. The
	// conditional UPDATE in the repository remains the authoritative guard
	// across processes and restarts.
	s.publishMu.Lock()
	defer s.publishMu.Unlock()

	draft, err := s.drafts.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("publish draft: %w", err)
	}
	if draft.Status == constants.DraftStatusPublished {
		return nil, ErrDraftAlreadyPublished
	}
	entries, err := s.drafts.ListEntries(ctx, draft.ID)
	if err != nil {
		return nil, fmt.Errorf("publish draft: load entries: %w", err)
	}

	// Check the draft for conflicts before touching the live timetable.
	if conflicts := s.detectConflicts(ctx, draftEntriesToSchedules(entries)); len(conflicts) > 0 {
		return nil, &DraftConflictError{Conflicts: conflicts}
	}

	publishedAt := time.Now()
	replacement := make([]model.Schedule, 0, len(entries))
	for i := range entries {
		replacement = append(replacement, model.Schedule{
			Week:        entries[i].Week,
			DayOfWeek:   entries[i].DayOfWeek,
			TimeSlotID:  entries[i].TimeSlotID,
			ClassroomID: entries[i].ClassroomID,
			TeacherID:   entries[i].TeacherID,
			ClassID:     entries[i].ClassID,
			CourseID:    entries[i].CourseID,
		})
	}
	if err := s.drafts.Publish(ctx, draft.ID, publishedBy, publishedAt, replacement); err != nil {
		if errors.Is(err, repository.ErrAlreadyPublished) {
			return nil, ErrDraftAlreadyPublished
		}
		return nil, fmt.Errorf("publish draft: %w", err)
	}
	s.logger.Info("schedule draft published",
		"draft_id", draft.ID, "name", draft.Name, "published_by", publishedBy, "entries", len(replacement))
	return &dto.PublishDraftResponse{
		DraftID:     draft.ID,
		Name:        draft.Name,
		Status:      constants.DraftStatusPublished,
		PublishedBy: publishedBy,
		PublishedAt: publishedAt.Format(time.RFC3339),
		Replaced:    len(replacement),
	}, nil
}

// scheduleToDraftEntry copies one live timetable entry into a draft snapshot entry.
func scheduleToDraftEntry(item model.Schedule) model.ScheduleDraftEntry {
	return model.ScheduleDraftEntry{
		Week:        item.Week,
		DayOfWeek:   item.DayOfWeek,
		TimeSlotID:  item.TimeSlotID,
		ClassroomID: item.ClassroomID,
		TeacherID:   item.TeacherID,
		ClassID:     item.ClassID,
		CourseID:    item.CourseID,
	}
}

// draftEntriesToSchedules converts snapshot entries to timetable-shaped values
// so enrichment and conflict detection can be reused. The entry ID is kept as
// the schedule ID so pairwise conflict checks never compare an entry to itself.
func draftEntriesToSchedules(entries []model.ScheduleDraftEntry) []model.Schedule {
	out := make([]model.Schedule, 0, len(entries))
	for i := range entries {
		item := model.Schedule{
			Week:        entries[i].Week,
			DayOfWeek:   entries[i].DayOfWeek,
			TimeSlotID:  entries[i].TimeSlotID,
			ClassroomID: entries[i].ClassroomID,
			TeacherID:   entries[i].TeacherID,
			ClassID:     entries[i].ClassID,
			CourseID:    entries[i].CourseID,
		}
		item.ID = entries[i].ID
		out = append(out, item)
	}
	return out
}

// draftResponse maps a draft model to its API representation.
func draftResponse(draft *model.ScheduleDraft) dto.DraftResponse {
	resp := dto.DraftResponse{
		ID:          draft.ID,
		Name:        draft.Name,
		Description: draft.Description,
		Status:      draft.Status,
		EntryCount:  draft.EntryCount,
		PublishedBy: draft.PublishedBy,
		CreatedAt:   draft.CreatedAt.Format(time.RFC3339),
	}
	if draft.PublishedAt != nil {
		formatted := draft.PublishedAt.Format(time.RFC3339)
		resp.PublishedAt = &formatted
	}
	return resp
}
