package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

func newDraftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Classroom{}, &model.Teacher{}, &model.Class{}, &model.Course{}, &model.TimeSlot{}, &model.Schedule{}, &model.AdjustmentLog{}, &model.ScheduleDraft{}, &model.ScheduleDraftEntry{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func draftEntries(draftID uint, n int) []model.ScheduleDraftEntry {
	entries := make([]model.ScheduleDraftEntry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, model.ScheduleDraftEntry{
			DraftID:     draftID,
			Week:        1,
			DayOfWeek:   i + 1,
			TimeSlotID:  1,
			ClassroomID: 1,
			TeacherID:   1,
			ClassID:     1,
			CourseID:    1,
		})
	}
	return entries
}

func TestScheduleDraftRepositoryCreateAndRead(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	repo := repository.NewScheduleDraftRepository(db)

	draft := &model.ScheduleDraft{Name: "v1", Description: "第一版", Status: constants.DraftStatusDraft, EntryCount: 2}
	if err := repo.CreateWithEntries(ctx, draft, draftEntries(0, 2)); err != nil {
		t.Fatalf("create with entries: %v", err)
	}
	if draft.ID == 0 {
		t.Fatal("expected auto generated id")
	}

	got, err := repo.GetByID(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Name != "v1" || got.Status != constants.DraftStatusDraft || got.EntryCount != 2 {
		t.Fatalf("unexpected draft: %+v", got)
	}

	entries, err := repo.ListEntries(ctx, draft.ID)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 || entries[0].DraftID != draft.ID {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	if err := repo.CreateWithEntries(ctx, &model.ScheduleDraft{Name: "v2", Status: constants.DraftStatusDraft}, nil); err != nil {
		t.Fatalf("create second draft: %v", err)
	}
	items, total, err := repo.List(ctx, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("expected total=2 len=1, got total=%d len=%d", total, len(items))
	}
}

func TestScheduleDraftRepositoryRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewScheduleDraftRepository(newDraftTestDB(t))

	if err := repo.CreateWithEntries(ctx, &model.ScheduleDraft{Name: "v1", Status: constants.DraftStatusDraft}, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := repo.CreateWithEntries(ctx, &model.ScheduleDraft{Name: "v1", Status: constants.DraftStatusDraft}, nil)
	if !errors.Is(err, repository.ErrConstraint) {
		t.Fatalf("expected ErrConstraint, got %v", err)
	}
}

func TestScheduleDraftRepositoryPublishSwapsSchedulesOnce(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	repo := repository.NewScheduleDraftRepository(db)

	draft := &model.ScheduleDraft{Name: "release", Status: constants.DraftStatusDraft, EntryCount: 1}
	if err := repo.CreateWithEntries(ctx, draft, draftEntries(0, 1)); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	// Pre-existing live timetable that must be replaced.
	if err := db.Create(&model.Schedule{Week: 9, DayOfWeek: 5, TimeSlotID: 1, ClassroomID: 1, TeacherID: 1, ClassID: 1, CourseID: 1}).Error; err != nil {
		t.Fatal(err)
	}

	replacement := []model.Schedule{{Week: 1, DayOfWeek: 1, TimeSlotID: 1, ClassroomID: 1, TeacherID: 1, ClassID: 1, CourseID: 1}}
	publishedAt := time.Now().Truncate(time.Second)
	if err := repo.Publish(ctx, draft.ID, "教务员A", publishedAt, replacement); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := repo.GetByID(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if got.Status != constants.DraftStatusPublished || got.PublishedBy != "教务员A" || got.PublishedAt == nil {
		t.Fatalf("publish metadata not stored: %+v", got)
	}

	var live []model.Schedule
	if err := db.Find(&live).Error; err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].Week != 1 {
		t.Fatalf("live timetable not replaced: %+v", live)
	}

	// A second publish of the same draft must lose the compare-and-swap and
	// leave the live timetable untouched.
	err = repo.Publish(ctx, draft.ID, "教务员B", time.Now(), []model.Schedule{{Week: 3, DayOfWeek: 3, TimeSlotID: 1, ClassroomID: 1, TeacherID: 1, ClassID: 1, CourseID: 1}})
	if !errors.Is(err, repository.ErrAlreadyPublished) {
		t.Fatalf("expected ErrAlreadyPublished, got %v", err)
	}
	live = nil
	if err := db.Find(&live).Error; err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].Week != 1 {
		t.Fatalf("live timetable changed after losing publish: %+v", live)
	}
	got, err = repo.GetByID(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if got.PublishedBy != "教务员A" {
		t.Fatalf("publisher overwritten by losing publish: %s", got.PublishedBy)
	}
}
