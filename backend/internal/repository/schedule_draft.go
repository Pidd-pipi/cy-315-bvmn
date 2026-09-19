package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
)

// ScheduleDraftRepository defines persistence operations for schedule drafts
// and their snapshot entries.
type ScheduleDraftRepository interface {
	CreateWithEntries(ctx context.Context, draft *model.ScheduleDraft, entries []model.ScheduleDraftEntry) error
	GetByID(ctx context.Context, id uint) (*model.ScheduleDraft, error)
	List(ctx context.Context, page, pageSize int) ([]model.ScheduleDraft, int64, error)
	ListEntries(ctx context.Context, draftID uint) ([]model.ScheduleDraftEntry, error)
	Publish(ctx context.Context, draftID uint, publishedBy string, publishedAt time.Time, schedules []model.Schedule) error
}

type scheduleDraftRepository struct {
	db *gorm.DB
}

// NewScheduleDraftRepository constructs a schedule draft repository.
func NewScheduleDraftRepository(db *gorm.DB) ScheduleDraftRepository {
	return &scheduleDraftRepository{db: db}
}

// CreateWithEntries stores a draft and its snapshot entries in one transaction.
func (r *scheduleDraftRepository) CreateWithEntries(ctx context.Context, draft *model.ScheduleDraft, entries []model.ScheduleDraftEntry) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(draft).Error; err != nil {
			if isConstraintError(err) {
				return ErrConstraint
			}
			return fmt.Errorf("create schedule draft: %w", err)
		}
		if len(entries) > 0 {
			for i := range entries {
				entries[i].DraftID = draft.ID
			}
			if err := tx.CreateInBatches(entries, 200).Error; err != nil {
				return fmt.Errorf("create schedule draft entries: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (r *scheduleDraftRepository) GetByID(ctx context.Context, id uint) (*model.ScheduleDraft, error) {
	var item model.ScheduleDraft
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &item, nil
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

func (r *scheduleDraftRepository) ListEntries(ctx context.Context, draftID uint) ([]model.ScheduleDraftEntry, error) {
	var items []model.ScheduleDraftEntry
	if err := r.db.WithContext(ctx).
		Where("draft_id = ?", draftID).
		Order("week ASC, day_of_week ASC, time_slot_id ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list schedule draft entries: %w", err)
	}
	return items, nil
}

// Publish atomically marks the draft as published and replaces the whole
// active timetable with the given entries. The conditional status update acts
// as a compare-and-swap: exactly one concurrent publish can win, every other
// call observes ErrAlreadyPublished and leaves the timetable untouched.
func (r *scheduleDraftRepository) Publish(ctx context.Context, draftID uint, publishedBy string, publishedAt time.Time, schedules []model.Schedule) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE schedule_drafts SET status = ?, published_by = ?, published_at = ?, updated_at = ? WHERE id = ? AND status = ? AND deleted_at IS NULL`,
			constants.DraftStatusPublished, publishedBy, publishedAt, time.Now(), draftID, constants.DraftStatusDraft,
		)
		if res.Error != nil {
			return fmt.Errorf("mark schedule draft published: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrAlreadyPublished
		}
		if err := tx.Where("1 = 1").Delete(&model.Schedule{}).Error; err != nil {
			return fmt.Errorf("replace schedules: clear current: %w", err)
		}
		if len(schedules) > 0 {
			if err := tx.CreateInBatches(schedules, 200).Error; err != nil {
				return fmt.Errorf("replace schedules: insert draft entries: %w", err)
			}
		}
		return nil
	})
}
