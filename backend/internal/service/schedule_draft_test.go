package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
	"github.com/gbschedule/gbschedule/internal/service"
)

type draftFixture struct {
	slot    *model.TimeSlot
	room    *model.Classroom
	teacher *model.Teacher
	class   *model.Class
	course  *model.Course
}

func seedDraftFixture(t *testing.T, db *gorm.DB) *draftFixture {
	t.Helper()
	f := &draftFixture{
		slot:    &model.TimeSlot{Code: "1", Name: "第一节", StartTime: "08:00", EndTime: "09:40"},
		room:    &model.Classroom{Code: "R301", Name: "301教室", Capacity: 50},
		teacher: &model.Teacher{Name: "张老师", EmployeeNo: "T001", Subjects: []string{"数学"}},
		class:   &model.Class{Name: "一班", StudentCount: 40, Grade: "高一"},
		course:  &model.Course{Name: "数学", Code: "MATH", Duration: 1},
	}
	for _, v := range []any{f.slot, f.room, f.teacher, f.class, f.course} {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}
	return f
}

func (f *draftFixture) entry(week uint, day int) model.Schedule {
	return model.Schedule{
		Week:        week,
		DayOfWeek:   day,
		TimeSlotID:  f.slot.ID,
		ClassroomID: f.room.ID,
		TeacherID:   f.teacher.ID,
		ClassID:     f.class.ID,
		CourseID:    f.course.ID,
	}
}

func liveSchedules(t *testing.T, db *gorm.DB) []model.Schedule {
	t.Helper()
	var items []model.Schedule
	if err := db.Order("week ASC, day_of_week ASC, time_slot_id ASC, id ASC").Find(&items).Error; err != nil {
		t.Fatalf("load live schedules: %v", err)
	}
	return items
}

func TestScheduleDraftSaveListAndDetail(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedDraftFixture(t, db)
	if err := db.Create(&[]model.Schedule{f.entry(1, 1), f.entry(1, 2)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newScheduleService(t, db)

	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "2026春季-v1", Description: "第一版"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if draft.ID == 0 || draft.Status != constants.DraftStatusDraft || draft.EntryCount != 2 {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	if _, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "2026春季-v2"}); err != nil {
		t.Fatalf("save second draft: %v", err)
	}
	if _, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "2026春季-v3"}); err != nil {
		t.Fatalf("save third draft: %v", err)
	}

	// Paginated listing.
	page1, total, err := svc.ListDrafts(ctx, 1, 2)
	if err != nil {
		t.Fatalf("list drafts page 1: %v", err)
	}
	if total != 3 || len(page1) != 2 {
		t.Fatalf("expected total=3 len=2, got total=%d len=%d", total, len(page1))
	}
	page2, _, err := svc.ListDrafts(ctx, 2, 2)
	if err != nil {
		t.Fatalf("list drafts page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 draft on page 2, got %d", len(page2))
	}

	// Detail carries the snapshot entries enriched with names.
	detail, err := svc.GetDraft(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if len(detail.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(detail.Entries))
	}
	if detail.Entries[0].TeacherName != "张老师" || detail.Entries[0].ClassName != "一班" || detail.Entries[0].CourseName != "数学" {
		t.Fatalf("entries not enriched: %+v", detail.Entries[0])
	}
}

func TestScheduleDraftSaveRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	seedDraftFixture(t, db)
	svc := newScheduleService(t, db)

	if _, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "v1"}); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "v1"}); !errors.Is(err, service.ErrDraftNameExists) {
		t.Fatalf("expected ErrDraftNameExists, got %v", err)
	}
	_, total, err := svc.ListDrafts(ctx, 1, 10)
	if err != nil {
		t.Fatalf("list drafts: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected only one draft after duplicate rejection, got %d", total)
	}
}

func TestScheduleDraftGetNotFound(t *testing.T) {
	ctx := context.Background()
	svc := newScheduleService(t, newScheduleTestDB(t))
	if _, err := svc.GetDraft(ctx, 999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := svc.PublishDraft(ctx, 999, &dto.PublishDraftRequest{PublishedBy: "admin"}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on publish, got %v", err)
	}
}

func TestScheduleDraftPublishReplacesScheduleAtomically(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedDraftFixture(t, db)
	if err := db.Create(&[]model.Schedule{f.entry(1, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newScheduleService(t, db)

	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "release-1"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// Mutate the live timetable after the snapshot; publish must restore the draft.
	if err := db.Where("1 = 1").Delete(&model.Schedule{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Schedule{f.entry(2, 3), f.entry(2, 4)}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := svc.PublishDraft(ctx, draft.ID, &dto.PublishDraftRequest{PublishedBy: "教务员A"})
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	if result.Status != constants.DraftStatusPublished || result.PublishedBy != "教务员A" || result.Replaced != 1 {
		t.Fatalf("unexpected publish result: %+v", result)
	}
	if result.PublishedAt == "" {
		t.Fatal("expected published_at to be recorded")
	}

	live := liveSchedules(t, db)
	if len(live) != 1 || live[0].Week != 1 || live[0].DayOfWeek != 1 {
		t.Fatalf("live schedule not replaced by draft: %+v", live)
	}

	// Publish metadata stays queryable afterwards.
	detail, err := svc.GetDraft(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if detail.Status != constants.DraftStatusPublished || detail.PublishedBy != "教务员A" || detail.PublishedAt == nil {
		t.Fatalf("publish metadata missing: %+v", detail.DraftResponse)
	}
}

func TestScheduleDraftPublishRejectsConflictsAndKeepsSchedule(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedDraftFixture(t, db)
	// Two lessons for the same teacher/class/room at the same time: conflicted snapshot.
	if err := db.Create(&[]model.Schedule{f.entry(1, 1), f.entry(1, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newScheduleService(t, db)
	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "conflicted"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// Replace the live timetable with a clean one before publishing.
	if err := db.Where("1 = 1").Delete(&model.Schedule{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Schedule{f.entry(1, 2)}).Error; err != nil {
		t.Fatal(err)
	}
	before := liveSchedules(t, db)

	_, err = svc.PublishDraft(ctx, draft.ID, &dto.PublishDraftRequest{PublishedBy: "教务员A"})
	if !errors.Is(err, service.ErrDraftHasConflicts) {
		t.Fatalf("expected ErrDraftHasConflicts, got %v", err)
	}
	var conflictErr *service.DraftConflictError
	if !errors.As(err, &conflictErr) || len(conflictErr.Conflicts) == 0 {
		t.Fatalf("expected conflict details, got %v", err)
	}

	after := liveSchedules(t, db)
	if len(after) != len(before) || after[0].ID != before[0].ID {
		t.Fatalf("live schedule changed after rejected publish: before=%+v after=%+v", before, after)
	}
	detail, err := svc.GetDraft(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if detail.Status != constants.DraftStatusDraft {
		t.Fatalf("draft status changed after rejected publish: %s", detail.Status)
	}
}

func TestScheduleDraftPublishTwiceFailsAndKeepsSchedule(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedDraftFixture(t, db)
	if err := db.Create(&[]model.Schedule{f.entry(1, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newScheduleService(t, db)
	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "release-1"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, draft.ID, &dto.PublishDraftRequest{PublishedBy: "教务员A"}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	before := liveSchedules(t, db)

	if _, err := svc.PublishDraft(ctx, draft.ID, &dto.PublishDraftRequest{PublishedBy: "教务员B"}); !errors.Is(err, service.ErrDraftAlreadyPublished) {
		t.Fatalf("expected ErrDraftAlreadyPublished, got %v", err)
	}
	after := liveSchedules(t, db)
	if len(after) != len(before) {
		t.Fatalf("live schedule changed after duplicate publish: before=%d after=%d", len(before), len(after))
	}
	detail, err := svc.GetDraft(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if detail.PublishedBy != "教务员A" {
		t.Fatalf("publisher overwritten by failed publish: %s", detail.PublishedBy)
	}
}

func TestScheduleDraftPublishConcurrentOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedDraftFixture(t, db)
	if err := db.Create(&[]model.Schedule{f.entry(1, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newScheduleService(t, db)
	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "race"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.PublishDraft(ctx, draft.ID, &dto.PublishDraftRequest{PublishedBy: fmt.Sprintf("admin-%d", i)})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	succeeded, alreadyPublished, other := 0, 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, service.ErrDraftAlreadyPublished):
			alreadyPublished++
		default:
			other++
			t.Errorf("unexpected publish error: %v", err)
		}
	}
	if succeeded != 1 || alreadyPublished != workers-1 {
		t.Fatalf("expected exactly 1 success and %d already-published, got success=%d already=%d other=%d", workers-1, succeeded, alreadyPublished, other)
	}

	live := liveSchedules(t, db)
	if len(live) != 1 || live[0].Week != 1 {
		t.Fatalf("live schedule corrupted after concurrent publish: %+v", live)
	}
	detail, err := svc.GetDraft(ctx, draft.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if detail.Status != constants.DraftStatusPublished || detail.PublishedBy == "" || detail.PublishedAt == nil {
		t.Fatalf("publish metadata missing after race: %+v", detail.DraftResponse)
	}
}

func TestScheduleDraftSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	open := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(dbPath+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		if err := db.AutoMigrate(&model.Classroom{}, &model.Teacher{}, &model.Class{}, &model.Course{}, &model.TimeSlot{}, &model.Schedule{}, &model.AdjustmentLog{}, &model.ScheduleDraft{}, &model.ScheduleDraftEntry{}); err != nil {
			t.Fatalf("migrate db: %v", err)
		}
		return db
	}
	close := func(t *testing.T, db *gorm.DB) {
		t.Helper()
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("unwrap db: %v", err)
		}
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}

	var draftID uint
	db := open(t)
	f := seedDraftFixture(t, db)
	if err := db.Create(&[]model.Schedule{f.entry(1, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewScheduleService(
		repository.NewScheduleRepository(db),
		repository.NewClassroomRepository(db),
		repository.NewTeacherRepository(db),
		repository.NewClassRepository(db),
		repository.NewCourseRepository(db),
		repository.NewTimeSlotRepository(db),
		repository.NewAdjustmentLogRepository(db),
		repository.NewScheduleDraftRepository(db),
		logger,
	)
	draft, err := svc.SaveDraft(ctx, &dto.SaveDraftRequest{Name: "durable"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	draftID = draft.ID
	if _, err := svc.PublishDraft(ctx, draftID, &dto.PublishDraftRequest{PublishedBy: "教务员A"}); err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	close(t, db)

	// Simulate a service restart: reopen the same database file from scratch.
	db = open(t)
	defer close(t, db)
	svc = service.NewScheduleService(
		repository.NewScheduleRepository(db),
		repository.NewClassroomRepository(db),
		repository.NewTeacherRepository(db),
		repository.NewClassRepository(db),
		repository.NewCourseRepository(db),
		repository.NewTimeSlotRepository(db),
		repository.NewAdjustmentLogRepository(db),
		repository.NewScheduleDraftRepository(db),
		logger,
	)
	detail, err := svc.GetDraft(ctx, draftID)
	if err != nil {
		t.Fatalf("get draft after restart: %v", err)
	}
	if detail.Status != constants.DraftStatusPublished || detail.PublishedBy != "教务员A" || detail.PublishedAt == nil {
		t.Fatalf("publish record lost after restart: %+v", detail.DraftResponse)
	}
	if len(detail.Entries) != 1 {
		t.Fatalf("draft entries lost after restart: %+v", detail.Entries)
	}
	if _, err := svc.PublishDraft(ctx, draftID, &dto.PublishDraftRequest{PublishedBy: "教务员B"}); !errors.Is(err, service.ErrDraftAlreadyPublished) {
		t.Fatalf("expected ErrDraftAlreadyPublished after restart, got %v", err)
	}
}
