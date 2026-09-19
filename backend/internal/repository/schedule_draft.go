package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
	"gorm.io/gorm"
)

// DraftSnapshot is the set of timetable entries stored for a new draft.
type DraftSnapshot struct {
	Name      string
	CreatedBy string
	Items     []model.Schedule
}

// ScheduleDraftRepository persists named timetable drafts, their snapshots and
// their publication records.
type ScheduleDraftRepository interface {
	// SaveSnapshot stores a draft and its items in a single transaction.
	// A duplicate draft name fails with ErrConstraint.
	SaveSnapshot(ctx context.Context, snapshot DraftSnapshot) (*model.ScheduleDraft, error)
	List(ctx context.Context, page, pageSize int) ([]model.ScheduleDraft, int64, error)
	GetByID(ctx context.Context, id uint) (*model.ScheduleDraft, error)
	ListItems(ctx context.Context, draftID uint) ([]model.ScheduleDraftItem, error)
	ListPublishes(ctx context.Context, draftID *uint, page, pageSize int) ([]model.SchedulePublishRecord, int64, error)

	// PublishTransactionally claims an unpublished draft, runs replace inside
	// the same transaction (it must atomically replace the current timetable
	// with items), records the publisher and writes the publish audit record.
	// It returns ErrAlreadyPublished when the draft is already in the
	// published state and ErrConcurrentPublish when a concurrent transaction
	// wins the atomic claim.
	PublishTransactionally(
		ctx context.Context,
		draftID uint,
		publishedBy string,
		items []model.ScheduleDraftItem,
		replace func(tx *gorm.DB) error,
	) (*model.SchedulePublishRecord, error)
}

type scheduleDraftRepository struct {
	db *gorm.DB
}

// NewScheduleDraftRepository constructs a schedule draft repository.
func NewScheduleDraftRepository(db *gorm.DB) ScheduleDraftRepository {
	return &scheduleDraftRepository{db: db}
}

func (r *scheduleDraftRepository) SaveSnapshot(ctx context.Context, snapshot DraftSnapshot) (*model.ScheduleDraft, error) {
	draft := &model.ScheduleDraft{
		Name:      snapshot.Name,
		Status:    constants.ScheduleDraftStatusDraft,
		ItemCount: len(snapshot.Items),
		CreatedBy: snapshot.CreatedBy,
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(draft).Error; err != nil {
			if isConstraintError(err) {
				return ErrConstraint
			}
			return fmt.Errorf("create schedule draft: %w", err)
		}
		if len(snapshot.Items) == 0 {
			return nil
		}
		items := make([]model.ScheduleDraftItem, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			items = append(items, model.ScheduleDraftItem{
				DraftID:     draft.ID,
				Week:        item.Week,
				DayOfWeek:   item.DayOfWeek,
				TimeSlotID:  item.TimeSlotID,
				ClassroomID: item.ClassroomID,
				TeacherID:   item.TeacherID,
				ClassID:     item.ClassID,
				CourseID:    item.CourseID,
			})
		}
		if err := tx.CreateInBatches(items, 200).Error; err != nil {
			return fmt.Errorf("create schedule draft items: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return draft, nil
}

func (r *scheduleDraftRepository) List(ctx context.Context, page, pageSize int) ([]model.ScheduleDraft, int64, error) {
	var items []model.ScheduleDraft
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.ScheduleDraft{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count schedule drafts: %w", err)
	}
	if err := paginate(r.db.WithContext(ctx).Model(&model.ScheduleDraft{}), page, pageSize).
		Order("id DESC").Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list schedule drafts: %w", err)
	}
	return items, total, nil
}

func (r *scheduleDraftRepository) GetByID(ctx context.Context, id uint) (*model.ScheduleDraft, error) {
	var item model.ScheduleDraft
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &item, nil
}

func (r *scheduleDraftRepository) ListItems(ctx context.Context, draftID uint) ([]model.ScheduleDraftItem, error) {
	var items []model.ScheduleDraftItem
	if err := r.db.WithContext(ctx).
		Where("draft_id = ?", draftID).
		Order("week ASC, day_of_week ASC, time_slot_id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list schedule draft items: %w", err)
	}
	return items, nil
}

func (r *scheduleDraftRepository) ListPublishes(ctx context.Context, draftID *uint, page, pageSize int) ([]model.SchedulePublishRecord, int64, error) {
	var items []model.SchedulePublishRecord
	var total int64
	query := r.db.WithContext(ctx).Model(&model.SchedulePublishRecord{})
	if draftID != nil {
		query = query.Where("draft_id = ?", *draftID)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count schedule publish records: %w", err)
	}
	if err := paginate(query, page, pageSize).Order("id DESC").Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list schedule publish records: %w", err)
	}
	return items, total, nil
}

func (r *scheduleDraftRepository) PublishTransactionally(
	ctx context.Context,
	draftID uint,
	publishedBy string,
	items []model.ScheduleDraftItem,
	replace func(tx *gorm.DB) error,
) (*model.SchedulePublishRecord, error) {
	now := time.Now().Unix()
	var record *model.SchedulePublishRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var draft model.ScheduleDraft
		if err := tx.First(&draft, draftID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock schedule draft: %w", err)
		}
		if draft.Status == constants.ScheduleDraftStatusPublished {
			return ErrAlreadyPublished
		}

		// Atomically claim the draft. With more than one concurrent publisher
		// exactly one statement affects a row; every other transaction gets
		// ErrConcurrentPublish and must not touch the timetable.
		result := tx.Model(&model.ScheduleDraft{}).
			Where("id = ? AND status = ?", draftID, constants.ScheduleDraftStatusDraft).
			Updates(map[string]any{
				"status":       constants.ScheduleDraftStatusPublished,
				"published_by": publishedBy,
				"published_at": now,
			})
		if result.Error != nil {
			return fmt.Errorf("claim schedule draft: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrConcurrentPublish
		}

		if err := replace(tx); err != nil {
			return fmt.Errorf("replace current timetable: %w", err)
		}

		record = &model.SchedulePublishRecord{
			DraftID:     draft.ID,
			DraftName:   draft.Name,
			PublishedBy: publishedBy,
			PublishedAt: now,
			ItemCount:   len(items),
		}
		if err := tx.Create(record).Error; err != nil {
			if isConstraintError(err) {
				// The unique(draft_id) constraint is the last line of defense
				// against publishing one draft twice.
				return ErrConcurrentPublish
			}
			return fmt.Errorf("create schedule publish record: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}
