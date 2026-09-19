package repository_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

func TestScheduleDraftConcurrentPublishOneWinner(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "c.db") + "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Classroom{}, &model.Teacher{}, &model.Class{}, &model.Course{},
		&model.TimeSlot{}, &model.Schedule{},
		&model.ScheduleDraft{}, &model.ScheduleDraftItem{}, &model.SchedulePublishRecord{},
	); err != nil {
		t.Fatal(err)
	}

	room := &model.Classroom{Code: "R1", Name: "r", Capacity: 1}
	teacher := &model.Teacher{Name: "t", EmployeeNo: "E1"}
	class := &model.Class{Name: "c"}
	course := &model.Course{Name: "k", Code: "K1"}
	slot := &model.TimeSlot{Code: "S1", Name: "s", StartTime: "1", EndTime: "2"}
	for _, v := range []any{room, teacher, class, course, slot} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&model.Schedule{Week: 1, DayOfWeek: 1, TimeSlotID: slot.ID, ClassroomID: room.ID, TeacherID: teacher.ID, ClassID: class.ID, CourseID: course.ID})

	repo := repository.NewScheduleDraftRepository(db)
	draft, err := repo.SaveSnapshot(context.Background(), repository.DraftSnapshot{
		Name: "d", Items: []model.Schedule{{Week: 1, DayOfWeek: 1, TimeSlotID: slot.ID, ClassroomID: room.ID, TeacherID: teacher.ID, ClassID: class.ID, CourseID: course.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = repo.PublishTransactionally(context.Background(), draft.ID, "admin", nil, func(tx *gorm.DB) error {
				return tx.Where("1=1").Delete(&model.Schedule{}).Error
			})
		}(i)
	}
	close(start)
	wg.Wait()

	ok, classified, other := 0, 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, repository.ErrAlreadyPublished) || errors.Is(e, repository.ErrConcurrentPublish):
			classified++
		default:
			other++
			fmt.Printf("UNEXPECTED: %v\n", e)
		}
	}
	t.Logf("ok=%d classified=%d other=%d (total %d)", ok, classified, other, n)
	if ok != 1 || other != 0 {
		t.Fatalf("expected exactly 1 success and 0 unexpected errors")
	}
}
