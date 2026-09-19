package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
	"gorm.io/gorm"
)

// ScheduleDraftService covers the draft-and-publish lifecycle of a timetable.
type ScheduleDraftService interface {
	// SaveDraft snapshots the current timetable under a unique name.
	SaveDraft(ctx context.Context, req *dto.CreateScheduleDraftRequest) (*dto.ScheduleDraftSummary, error)
	ListDrafts(ctx context.Context, page, pageSize int) ([]dto.ScheduleDraftSummary, int64, error)
	GetDraft(ctx context.Context, id uint) (*dto.ScheduleDraftDetailResponse, error)
	CheckDraftConflicts(ctx context.Context, id uint) ([]dto.ConflictResponse, error)
	// Publish validates the draft and replaces the current timetable in one
	// transaction, recording the publisher and the publication time.
	Publish(ctx context.Context, id uint, req *dto.PublishScheduleDraftRequest) (*dto.PublishScheduleDraftResponse, error)
	ListPublishes(ctx context.Context, draftID *uint, page, pageSize int) ([]dto.SchedulePublishRecordResponse, int64, error)
}

type scheduleDraftService struct {
	drafts    repository.ScheduleDraftRepository
	schedules repository.ScheduleRepository
	entities  scheduleEntities
	logger    *slog.Logger
}

// NewScheduleDraftService constructs a schedule draft service.
func NewScheduleDraftService(
	drafts repository.ScheduleDraftRepository,
	schedules repository.ScheduleRepository,
	classrooms repository.ClassroomRepository,
	teachers repository.TeacherRepository,
	classes repository.ClassRepository,
	courses repository.CourseRepository,
	timeSlots repository.TimeSlotRepository,
	logger *slog.Logger,
) ScheduleDraftService {
	return &scheduleDraftService{
		drafts:    drafts,
		schedules: schedules,
		entities:  newScheduleEntities(classrooms, teachers, classes, courses, timeSlots),
		logger:    logger,
	}
}

func (s *scheduleDraftService) SaveDraft(ctx context.Context, req *dto.CreateScheduleDraftRequest) (*dto.ScheduleDraftSummary, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("save draft: %w: name must not be blank", ErrInvalid)
	}
	items, err := s.schedules.List(ctx, repository.ScheduleFilter{})
	if err != nil {
		return nil, fmt.Errorf("load current schedules: %w", err)
	}
	draft, err := s.drafts.SaveSnapshot(ctx, repository.DraftSnapshot{
		Name:      name,
		CreatedBy: strings.TrimSpace(req.CreatedBy),
		Items:     items,
	})
	if err != nil {
		if errors.Is(err, repository.ErrConstraint) {
			return nil, NewDetailError(ErrConflict, fmt.Sprintf("draft name %q already exists", name))
		}
		return nil, fmt.Errorf("save draft snapshot: %w", err)
	}
	summary := draftSummary(draft)
	return &summary, nil
}

func (s *scheduleDraftService) ListDrafts(ctx context.Context, page, pageSize int) ([]dto.ScheduleDraftSummary, int64, error) {
	items, total, err := s.drafts.List(ctx, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list drafts: %w", err)
	}
	out := make([]dto.ScheduleDraftSummary, 0, len(items))
	for i := range items {
		out = append(out, draftSummary(&items[i]))
	}
	return out, total, nil
}

func (s *scheduleDraftService) GetDraft(ctx context.Context, id uint) (*dto.ScheduleDraftDetailResponse, error) {
	draft, err := s.loadDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := s.drafts.ListItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list draft items: %w", err)
	}
	schedules := draftItemsToSchedules(items)
	enriched, err := s.entities.enrichSchedules(ctx, schedules)
	if err != nil {
		return nil, err
	}
	detail := &dto.ScheduleDraftDetailResponse{
		ScheduleDraftSummary: draftSummary(draft),
		Items:                scheduleResponsesToDraftItems(enriched),
	}
	return detail, nil
}

func (s *scheduleDraftService) CheckDraftConflicts(ctx context.Context, id uint) ([]dto.ConflictResponse, error) {
	draft, err := s.loadDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := s.drafts.ListItems(ctx, draft.ID)
	if err != nil {
		return nil, fmt.Errorf("list draft items: %w", err)
	}
	conflicts, err := s.entities.detectConflicts(ctx, draftItemsToSchedules(items))
	if err != nil {
		return nil, err
	}
	return conflicts, nil
}

func (s *scheduleDraftService) Publish(ctx context.Context, id uint, req *dto.PublishScheduleDraftRequest) (*dto.PublishScheduleDraftResponse, error) {
	publishedBy := strings.TrimSpace(req.PublishedBy)
	if publishedBy == "" {
		return nil, fmt.Errorf("publish draft: %w: published_by must not be blank", ErrInvalid)
	}

	draft, err := s.loadDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	if draft.Status == constants.ScheduleDraftStatusPublished {
		return nil, NewDetailError(ErrConflict, fmt.Sprintf("draft %q has already been published", draft.Name))
	}

	items, err := s.drafts.ListItems(ctx, draft.ID)
	if err != nil {
		return nil, fmt.Errorf("list draft items: %w", err)
	}
	schedules := draftItemsToSchedules(items)

	// Pre-check conflicts before opening the write transaction so a rejected
	// publication never mutates the current timetable.
	conflicts, err := s.entities.detectConflicts(ctx, schedules)
	if err != nil {
		return nil, fmt.Errorf("check draft conflicts: %w", err)
	}
	if len(conflicts) > 0 {
		return nil, &ScheduleConflictsError{Conflicts: conflicts}
	}

	record, err := s.drafts.PublishTransactionally(ctx, draft.ID, publishedBy, items, func(tx *gorm.DB) error {
		return repository.ReplaceSchedulesInTx(tx, schedules)
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, ErrNotFound
		case errors.Is(err, repository.ErrAlreadyPublished):
			return nil, NewDetailError(ErrConflict, fmt.Sprintf("draft %q has already been published", draft.Name))
		case errors.Is(err, repository.ErrConcurrentPublish):
			return nil, NewDetailError(ErrConflict, fmt.Sprintf("draft %q was published by a concurrent request", draft.Name))
		default:
			return nil, fmt.Errorf("publish draft: %w", err)
		}
	}

	enriched, err := s.entities.enrichSchedules(ctx, schedules)
	if err != nil {
		return nil, fmt.Errorf("enrich published schedules: %w", err)
	}

	return &dto.PublishScheduleDraftResponse{
		PublishID:   record.ID,
		DraftID:     record.DraftID,
		DraftName:   record.DraftName,
		Status:      constants.ScheduleDraftStatusPublished,
		PublishedBy: record.PublishedBy,
		PublishedAt: formatUnix(record.PublishedAt),
		ItemCount:   record.ItemCount,
		Schedules:   enriched,
	}, nil
}

func (s *scheduleDraftService) ListPublishes(ctx context.Context, draftID *uint, page, pageSize int) ([]dto.SchedulePublishRecordResponse, int64, error) {
	items, total, err := s.drafts.ListPublishes(ctx, draftID, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list publish records: %w", err)
	}
	out := make([]dto.SchedulePublishRecordResponse, 0, len(items))
	for i := range items {
		out = append(out, dto.SchedulePublishRecordResponse{
			ID:          items[i].ID,
			DraftID:     items[i].DraftID,
			DraftName:   items[i].DraftName,
			PublishedBy: items[i].PublishedBy,
			PublishedAt: formatUnix(items[i].PublishedAt),
			ItemCount:   items[i].ItemCount,
		})
	}
	return out, total, nil
}

func (s *scheduleDraftService) loadDraft(ctx context.Context, id uint) (*model.ScheduleDraft, error) {
	draft, err := s.drafts.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewDetailError(ErrNotFound, fmt.Sprintf("draft %d not found", id))
		}
		return nil, fmt.Errorf("get draft: %w", err)
	}
	return draft, nil
}

func draftSummary(draft *model.ScheduleDraft) dto.ScheduleDraftSummary {
	summary := dto.ScheduleDraftSummary{
		ID:          draft.ID,
		Name:        draft.Name,
		Status:      draft.Status,
		ItemCount:   draft.ItemCount,
		CreatedBy:   draft.CreatedBy,
		CreatedAt:   draft.CreatedAt.Format(time.RFC3339),
		PublishedBy: draft.PublishedBy,
	}
	if draft.PublishedAt != nil {
		summary.PublishedAt = formatUnix(*draft.PublishedAt)
	}
	return summary
}

func draftItemsToSchedules(items []model.ScheduleDraftItem) []model.Schedule {
	out := make([]model.Schedule, 0, len(items))
	for _, item := range items {
		out = append(out, model.Schedule{
			Model:       gorm.Model{ID: item.ID},
			Week:        item.Week,
			DayOfWeek:   item.DayOfWeek,
			TimeSlotID:  item.TimeSlotID,
			ClassroomID: item.ClassroomID,
			TeacherID:   item.TeacherID,
			ClassID:     item.ClassID,
			CourseID:    item.CourseID,
		})
	}
	return out
}

func scheduleResponsesToDraftItems(items []dto.ScheduleResponse) []dto.ScheduleDraftItemResponse {
	out := make([]dto.ScheduleDraftItemResponse, 0, len(items))
	for _, item := range items {
		out = append(out, dto.ScheduleDraftItemResponse{
			ID:            item.ID,
			Week:          item.Week,
			DayOfWeek:     item.DayOfWeek,
			TimeSlotID:    item.TimeSlotID,
			TimeSlotCode:  item.TimeSlotCode,
			TimeSlotName:  item.TimeSlotName,
			StartTime:     item.StartTime,
			EndTime:       item.EndTime,
			ClassroomID:   item.ClassroomID,
			ClassroomName: item.ClassroomName,
			TeacherID:     item.TeacherID,
			TeacherName:   item.TeacherName,
			ClassID:       item.ClassID,
			ClassName:     item.ClassName,
			CourseID:      item.CourseID,
			CourseName:    item.CourseName,
		})
	}
	return out
}

func formatUnix(ts int64) string {
	return time.Unix(ts, 0).Format(time.RFC3339)
}
