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
	slot     *model.TimeSlot
	room     *model.Classroom
	teacher  *model.Teacher
	class    *model.Class
	course   *model.Course
	room2    *model.Classroom
	teacher2 *model.Teacher
}

func newDraftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Shared-cache in-memory databases report SQLITE_LOCKED (not retryable via
	// busy_timeout) on concurrent writers; a single connection serializes them
	// while the conditional UPDATE claim still enforces one-winner semantics.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Classroom{}, &model.Teacher{}, &model.Class{}, &model.Course{},
		&model.TimeSlot{}, &model.Schedule{}, &model.AdjustmentLog{},
		&model.ScheduleDraft{}, &model.ScheduleDraftItem{}, &model.SchedulePublishRecord{},
	); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func newDraftServices(t *testing.T, db *gorm.DB) (service.ScheduleService, service.ScheduleDraftService) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	scheduleRepo := repository.NewScheduleRepository(db)
	classroomRepo := repository.NewClassroomRepository(db)
	teacherRepo := repository.NewTeacherRepository(db)
	classRepo := repository.NewClassRepository(db)
	courseRepo := repository.NewCourseRepository(db)
	timeSlotRepo := repository.NewTimeSlotRepository(db)
	adjustmentRepo := repository.NewAdjustmentLogRepository(db)
	draftRepo := repository.NewScheduleDraftRepository(db)

	scheduleSvc := service.NewScheduleService(scheduleRepo, classroomRepo, teacherRepo, classRepo, courseRepo, timeSlotRepo, adjustmentRepo, logger)
	draftSvc := service.NewScheduleDraftService(draftRepo, scheduleRepo, classroomRepo, teacherRepo, classRepo, courseRepo, timeSlotRepo, logger)
	return scheduleSvc, draftSvc
}

func seedDraftFixture(t *testing.T, db *gorm.DB) draftFixture {
	t.Helper()
	f := draftFixture{
		slot:     &model.TimeSlot{Code: "1", Name: "第一节", StartTime: "08:00", EndTime: "09:40"},
		room:     &model.Classroom{Code: "R301", Name: "301教室", Capacity: 50},
		room2:    &model.Classroom{Code: "R302", Name: "302教室", Capacity: 50},
		teacher:  &model.Teacher{Name: "张老师", EmployeeNo: "T001", Subjects: []string{"数学"}},
		teacher2: &model.Teacher{Name: "李老师", EmployeeNo: "T002", Subjects: []string{"语文"}},
		class:    &model.Class{Name: "一班", StudentCount: 40, Grade: "高一"},
		course:   &model.Course{Name: "数学", Code: "MATH", Duration: 1},
	}
	for _, v := range []any{f.slot, f.room, f.room2, f.teacher, f.teacher2, f.class, f.course} {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}
	return f
}

func mustGenerate(t *testing.T, ctx context.Context, svc service.ScheduleService, f draftFixture) {
	t.Helper()
	resp, err := svc.Generate(ctx, &dto.GenerateScheduleRequest{
		Weeks: 1, DaysPerWeek: 1, PeriodsPerDay: 1,
		Courses:      []dto.CourseRequirement{{CourseID: f.course.ID, WeeklyPeriods: 1, ClassID: f.class.ID, TeacherID: f.teacher.ID}},
		TeacherIDs:   []uint{f.teacher.ID},
		ClassIDs:     []uint{f.class.ID},
		ClassroomIDs: []uint{f.room.ID},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp.Generated != 1 || len(resp.Conflicts) != 0 {
		t.Fatalf("unexpected generate result: %+v", resp)
	}
}

func TestDraftSaveListGetPublishHappyPath(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)

	created, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "期中考试前课表", CreatedBy: "admin01"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if created.ID == 0 || created.ItemCount != 1 || created.Status != constants.ScheduleDraftStatusDraft || created.CreatedBy != "admin01" {
		t.Fatalf("unexpected draft summary: %+v", created)
	}

	// Pagination.
	list, total, err := draftSvc.ListDrafts(ctx, 1, 10)
	if err != nil || total != 1 || len(list) != 1 || list[0].Name != "期中考试前课表" {
		t.Fatalf("list drafts: items=%v total=%d err=%v", list, total, err)
	}

	detail, err := draftSvc.GetDraft(ctx, created.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if len(detail.Items) != 1 || detail.Items[0].TeacherName != "张老师" || detail.Items[0].ClassroomName != "301教室" {
		t.Fatalf("unexpected draft detail: %+v", detail)
	}

	conflicts, err := draftSvc.CheckDraftConflicts(ctx, created.ID)
	if err != nil {
		t.Fatalf("check draft conflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}

	// Change the current timetable before publishing so replacement is visible.
	if _, err := scheduleSvc.Generate(ctx, &dto.GenerateScheduleRequest{
		Weeks: 1, DaysPerWeek: 1, PeriodsPerDay: 1,
		Courses:      []dto.CourseRequirement{{CourseID: f.course.ID, WeeklyPeriods: 1, ClassID: f.class.ID, TeacherID: f.teacher2.ID}},
		TeacherIDs:   []uint{f.teacher2.ID},
		ClassIDs:     []uint{f.class.ID},
		ClassroomIDs: []uint{f.room2.ID},
	}); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	current, _ := scheduleSvc.List(ctx, nil, nil, nil, nil)
	if len(current) != 1 || current[0].TeacherID != f.teacher2.ID {
		t.Fatalf("precondition failed: %+v", current)
	}

	published, err := draftSvc.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "principal"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Status != constants.ScheduleDraftStatusPublished || published.PublishedBy != "principal" ||
		published.PublishedAt == "" || published.ItemCount != 1 || published.PublishID == 0 {
		t.Fatalf("unexpected publish response: %+v", published)
	}
	if len(published.Schedules) != 1 || published.Schedules[0].TeacherID != f.teacher.ID ||
		published.Schedules[0].ClassroomID != f.room.ID {
		t.Fatalf("published schedules not from draft: %+v", published.Schedules)
	}

	// Current timetable now equals the draft.
	after, _ := scheduleSvc.List(ctx, nil, nil, nil, nil)
	if len(after) != 1 || after[0].TeacherID != f.teacher.ID || after[0].ClassroomID != f.room.ID {
		t.Fatalf("current timetable was not replaced by draft: %+v", after)
	}
	// The response must echo the newly inserted timetable row, not the
	// draft-item snapshot ID.
	if published.Schedules[0].ID != after[0].ID || published.Schedules[0].ID == 0 {
		t.Fatalf("published schedule id %d does not match current row id %d", published.Schedules[0].ID, after[0].ID)
	}

	// Publish record is queryable afterwards.
	records, total, err := draftSvc.ListPublishes(ctx, &created.ID, 1, 10)
	if err != nil || total != 1 || len(records) != 1 ||
		records[0].PublishedBy != "principal" || records[0].DraftName != "期中考试前课表" {
		t.Fatalf("publish records: %+v total=%d err=%v", records, total, err)
	}

	detail2, err := draftSvc.GetDraft(ctx, created.ID)
	if err != nil || detail2.Status != constants.ScheduleDraftStatusPublished ||
		detail2.PublishedBy != "principal" || detail2.PublishedAt == "" {
		t.Fatalf("draft status not updated: %+v err=%v", detail2, err)
	}
}

func TestDraftDuplicateNameFails(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)

	if _, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "v1"}); err != nil {
		t.Fatalf("save first draft: %v", err)
	}
	_, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "v1"})
	if !errors.Is(err, service.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate name, got %v", err)
	}
	drafts, total, _ := draftSvc.ListDrafts(ctx, 1, 10)
	if total != 1 || len(drafts) != 1 {
		t.Fatalf("failed save must not create a draft: total=%d", total)
	}
}

func TestDraftOperationsOnMissingDraftFail(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	_, draftSvc := newDraftServices(t, db)

	if _, err := draftSvc.GetDraft(ctx, 999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("GetDraft expected ErrNotFound, got %v", err)
	}
	if _, err := draftSvc.CheckDraftConflicts(ctx, 999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("CheckDraftConflicts expected ErrNotFound, got %v", err)
	}
	_, err := draftSvc.Publish(ctx, 999, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("Publish expected ErrNotFound, got %v", err)
	}
}

func TestPublishTwiceFails(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)

	created, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "v1"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := draftSvc.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	_, err = draftSvc.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	if !errors.Is(err, service.ErrConflict) {
		t.Fatalf("second publish expected ErrConflict, got %v", err)
	}
	_, total, _ := draftSvc.ListPublishes(ctx, &created.ID, 1, 10)
	if total != 1 {
		t.Fatalf("expected exactly one publish record, got %d", total)
	}
}

func TestPublishConflictingDraftFailsAndKeepsCurrent(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)

	created, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "bad-draft"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// Tamper the snapshot to create a teacher/class/classroom collision.
	if err := db.Exec(
		"INSERT INTO schedule_draft_items (created_at, updated_at, draft_id, week, day_of_week, time_slot_id, classroom_id, teacher_id, class_id, course_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"2026-09-19 00:00:00", "2026-09-19 00:00:00", created.ID, 1, 1, f.slot.ID, f.room.ID, f.teacher.ID, f.class.ID, f.course.ID,
	).Error; err != nil {
		t.Fatalf("seed conflicting item: %v", err)
	}

	before, _ := scheduleSvc.List(ctx, nil, nil, nil, nil)
	_, err = draftSvc.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	var conflictErr *service.ScheduleConflictsError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ScheduleConflictsError, got %v", err)
	}
	if len(conflictErr.Conflicts) == 0 {
		t.Fatalf("expected conflict details, got none")
	}
	// Draft stays unpublished.
	detail, _ := draftSvc.GetDraft(ctx, created.ID)
	if detail.Status != constants.ScheduleDraftStatusDraft {
		t.Fatalf("conflicting draft must stay in draft status, got %s", detail.Status)
	}
	// No publish record.
	_, total, _ := draftSvc.ListPublishes(ctx, &created.ID, 1, 10)
	if total != 0 {
		t.Fatalf("conflicting publish must leave no record, got %d", total)
	}
	// Current timetable unchanged.
	after, _ := scheduleSvc.List(ctx, nil, nil, nil, nil)
	if len(after) != len(before) || after[0].ID != before[0].ID {
		t.Fatalf("current timetable changed after rejected publish: before=%+v after=%+v", before, after)
	}
}

func TestConcurrentPublishSucceedsOnce(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)

	const n = 8
	drafts := make([]uint, n)
	for i := 0; i < n; i++ {
		created, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: fmt.Sprintf("draft-%d", i)})
		if err != nil {
			t.Fatalf("save draft %d: %v", i, err)
		}
		drafts[i] = created.ID
	}

	// Hammer the same draft with concurrent publishers.
	var wg sync.WaitGroup
	var okCount, failCount int64
	var mu sync.Mutex
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := draftSvc.Publish(ctx, drafts[0], &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
			mu.Lock()
			if err == nil {
				okCount++
			} else if errors.Is(err, service.ErrConflict) {
				failCount++
			} else {
				t.Errorf("unexpected publish error: %v", err)
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if okCount != 1 || failCount != n-1 {
		t.Fatalf("expected exactly 1 success and %d conflicts, got %d/%d", n-1, okCount, failCount)
	}
	_, total, _ := draftSvc.ListPublishes(ctx, &drafts[0], 1, 10)
	if total != 1 {
		t.Fatalf("expected one publish record, got %d", total)
	}
	// Current timetable still intact (replaced with identical content once).
	after, err := scheduleSvc.List(ctx, nil, nil, nil, nil)
	if err != nil || len(after) != 1 {
		t.Fatalf("current timetable corrupted: %+v err=%v", after, err)
	}
}

func TestPublishResultSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gbschedule.db")
	ctx := context.Background()

	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{TranslateError: true})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		if err := db.AutoMigrate(
			&model.Classroom{}, &model.Teacher{}, &model.Class{}, &model.Course{},
			&model.TimeSlot{}, &model.Schedule{}, &model.AdjustmentLog{},
			&model.ScheduleDraft{}, &model.ScheduleDraftItem{}, &model.SchedulePublishRecord{},
		); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return db
	}

	db1 := open()
	scheduleSvc1, draftSvc1 := newDraftServices(t, db1)
	f := seedDraftFixture(t, db1)
	mustGenerate(t, ctx, scheduleSvc1, f)
	created, err := draftSvc1.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "restart-check", CreatedBy: "admin"})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	published, err := draftSvc1.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	sqlDB, _ := db1.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// Simulate service restart with a fresh process/connection.
	db2 := open()
	_, draftSvc2 := newDraftServices(t, db2)
	scheduleRepo2 := repository.NewScheduleRepository(db2)

	detail, err := draftSvc2.GetDraft(ctx, created.ID)
	if err != nil {
		t.Fatalf("get draft after restart: %v", err)
	}
	if detail.Status != constants.ScheduleDraftStatusPublished || detail.PublishedBy != "admin" ||
		detail.PublishedAt != published.PublishedAt {
		t.Fatalf("draft state not durable: %+v vs %+v", detail, published)
	}
	records, total, err := draftSvc2.ListPublishes(ctx, &created.ID, 1, 10)
	if err != nil || total != 1 || records[0].ID != published.PublishID {
		t.Fatalf("publish record not durable: %+v total=%d err=%v", records, total, err)
	}

	// Republish must still fail after restart.
	_, err = draftSvc2.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	if !errors.Is(err, service.ErrConflict) {
		t.Fatalf("republish after restart expected ErrConflict, got %v", err)
	}

	// Current timetable content persisted.
	current, err := scheduleRepo2.List(ctx, repository.ScheduleFilter{})
	if err != nil || len(current) != 1 ||
		current[0].TeacherID != f.teacher.ID || current[0].ClassroomID != f.room.ID {
		t.Fatalf("timetable not durable across restart: %+v err=%v", current, err)
	}
}

func TestPublishEmptyDraftClearsCurrentTimetable(t *testing.T) {
	ctx := context.Background()
	db := newDraftTestDB(t)
	scheduleSvc, draftSvc := newDraftServices(t, db)
	f := seedDraftFixture(t, db)
	mustGenerate(t, ctx, scheduleSvc, f)
	if n, _ := scheduleSvc.List(ctx, nil, nil, nil, nil); len(n) != 1 {
		t.Fatalf("precondition: expected 1 current row")
	}

	// Delete all current entries, then snapshot the now-empty timetable.
	if err := db.Where("1 = 1").Delete(&model.Schedule{}).Error; err != nil {
		t.Fatal(err)
	}
	created, err := draftSvc.SaveDraft(ctx, &dto.CreateScheduleDraftRequest{Name: "empty"})
	if err != nil {
		t.Fatalf("save empty draft: %v", err)
	}
	if created.ItemCount != 0 {
		t.Fatalf("expected 0 items, got %d", created.ItemCount)
	}
	// Put a row back so the replacement must observably clear it.
	mustGenerate(t, ctx, scheduleSvc, f)

	published, err := draftSvc.Publish(ctx, created.ID, &dto.PublishScheduleDraftRequest{PublishedBy: "admin"})
	if err != nil {
		t.Fatalf("publish empty draft: %v", err)
	}
	if published.ItemCount != 0 || len(published.Schedules) != 0 {
		t.Fatalf("expected empty publish result, got %+v", published)
	}
	current, err := scheduleSvc.List(ctx, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 0 {
		t.Fatalf("expected current timetable cleared, got %d rows", len(current))
	}
}
