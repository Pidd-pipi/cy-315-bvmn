package service

import (
	"context"
	"fmt"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

// scheduleEntities bundles the repositories needed to enrich timetable
// entries and to run conflict detection. It is shared by the current timetable
// service and the draft publish service.
type scheduleEntities struct {
	classrooms repository.ClassroomRepository
	teachers   repository.TeacherRepository
	classes    repository.ClassRepository
	courses    repository.CourseRepository
	timeSlots  repository.TimeSlotRepository
}

func newScheduleEntities(
	classrooms repository.ClassroomRepository,
	teachers repository.TeacherRepository,
	classes repository.ClassRepository,
	courses repository.CourseRepository,
	timeSlots repository.TimeSlotRepository,
) scheduleEntities {
	return scheduleEntities{
		classrooms: classrooms,
		teachers:   teachers,
		classes:    classes,
		courses:    courses,
		timeSlots:  timeSlots,
	}
}

func (e scheduleEntities) enrichSchedules(ctx context.Context, items []model.Schedule) ([]dto.ScheduleResponse, error) {
	timeSlots, _, err := e.timeSlots.List(ctx, 1, constants.MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("load time slots: %w", err)
	}
	slotMap := entityMap(timeSlots, func(sl model.TimeSlot) uint { return sl.ID })

	classroomList, err := e.classrooms.GetByIDs(ctx, uniqueClassroomIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load classrooms: %w", err)
	}
	classroomMap := entityMap(classroomList, func(c model.Classroom) uint { return c.ID })

	teacherList, err := e.teachers.GetByIDs(ctx, uniqueTeacherIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load teachers: %w", err)
	}
	teacherMap := entityMap(teacherList, func(t model.Teacher) uint { return t.ID })

	classList, err := e.classes.GetByIDs(ctx, uniqueClassIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load classes: %w", err)
	}
	classMap := entityMap(classList, func(c model.Class) uint { return c.ID })

	courseList, err := e.courses.GetByIDs(ctx, uniqueUint(items, func(s model.Schedule) uint { return s.CourseID }))
	if err != nil {
		return nil, fmt.Errorf("load courses: %w", err)
	}
	courseMap := entityMap(courseList, func(c model.Course) uint { return c.ID })

	return enrichSchedules(items, slotMap, classroomMap, teacherMap, classMap, courseMap), nil
}

// detectConflicts runs the full conflict detector against arbitrary timetable
// entries (the current timetable or an unpublished draft snapshot). Each
// colliding pair is reported once; entries are identified by their slice
// position so unsaved items (id == 0) are handled correctly as well.
func (e scheduleEntities) detectConflicts(ctx context.Context, items []model.Schedule) ([]dto.ConflictResponse, error) {
	classes, err := e.classes.GetByIDs(ctx, uniqueClassIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load classes for conflict check: %w", err)
	}
	classroomList, err := e.classrooms.GetByIDs(ctx, uniqueClassroomIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load classrooms for conflict check: %w", err)
	}
	teacherList, err := e.teachers.GetByIDs(ctx, uniqueTeacherIDs(items))
	if err != nil {
		return nil, fmt.Errorf("load teachers for conflict check: %w", err)
	}
	slotList, _, err := e.timeSlots.List(ctx, 1, constants.MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("load time slots for conflict check: %w", err)
	}

	classMap := entityMap(classes, func(c model.Class) uint { return c.ID })
	classroomMap := entityMap(classroomList, func(c model.Classroom) uint { return c.ID })
	teacherMap := entityMap(teacherList, func(t model.Teacher) uint { return t.ID })
	slotMap := entityMap(slotList, func(sl model.TimeSlot) uint { return sl.ID })

	teacherAt := map[string]int{}
	classAt := map[string]int{}
	classroomAt := map[string]int{}

	out := make([]dto.ConflictResponse, 0)

	for idx, item := range items {
		slotKey := fmt.Sprintf("%d-%d-%d", item.Week, item.DayOfWeek, item.TimeSlotID)

		if first, ok := teacherAt[fmt.Sprintf("%s-t-%d", slotKey, item.TeacherID)]; ok {
			out = append(out, dto.ConflictResponse{
				Type: constants.ConflictTeacherTime, EntityType: "teacher", EntityID: item.TeacherID,
				EntityName: teacherMap[item.TeacherID].Name, Week: item.Week, DayOfWeek: item.DayOfWeek, TimeSlotID: item.TimeSlotID,
				Suggestion: fmt.Sprintf("teacher already has a lesson at week %d day %d slot %d; move one of the lessons (entries #%d and #%d)", item.Week, item.DayOfWeek, item.TimeSlotID, first+1, idx+1),
			})
		} else {
			teacherAt[fmt.Sprintf("%s-t-%d", slotKey, item.TeacherID)] = idx
		}

		if first, ok := classAt[fmt.Sprintf("%s-c-%d", slotKey, item.ClassID)]; ok {
			out = append(out, dto.ConflictResponse{
				Type: constants.ConflictClassTime, EntityType: "class", EntityID: item.ClassID,
				EntityName: classMap[item.ClassID].Name, Week: item.Week, DayOfWeek: item.DayOfWeek, TimeSlotID: item.TimeSlotID,
				Suggestion: fmt.Sprintf("class already has a lesson at week %d day %d slot %d; move one of the lessons (entries #%d and #%d)", item.Week, item.DayOfWeek, item.TimeSlotID, first+1, idx+1),
			})
		} else {
			classAt[fmt.Sprintf("%s-c-%d", slotKey, item.ClassID)] = idx
		}

		if first, ok := classroomAt[fmt.Sprintf("%s-r-%d", slotKey, item.ClassroomID)]; ok {
			out = append(out, dto.ConflictResponse{
				Type: constants.ConflictClassroomTime, EntityType: "classroom", EntityID: item.ClassroomID,
				EntityName: classroomMap[item.ClassroomID].Name, Week: item.Week, DayOfWeek: item.DayOfWeek, TimeSlotID: item.TimeSlotID,
				Suggestion: fmt.Sprintf("classroom already has a lesson at week %d day %d slot %d; move one of the lessons (entries #%d and #%d)", item.Week, item.DayOfWeek, item.TimeSlotID, first+1, idx+1),
			})
		} else {
			classroomAt[fmt.Sprintf("%s-r-%d", slotKey, item.ClassroomID)] = idx
		}

		if class, ok := classMap[item.ClassID]; ok {
			if classroom, ok2 := classroomMap[item.ClassroomID]; ok2 && class.StudentCount > classroom.Capacity {
				out = append(out, dto.ConflictResponse{
					Type: constants.ConflictClassroomCap, EntityType: "class", EntityID: item.ClassID,
					EntityName: class.Name, Week: item.Week, DayOfWeek: item.DayOfWeek, TimeSlotID: item.TimeSlotID,
					Suggestion: fmt.Sprintf("class size %d exceeds classroom capacity %d; choose a larger classroom", class.StudentCount, classroom.Capacity),
				})
			}
		}
		if teacher, ok := teacherMap[item.TeacherID]; ok {
			if slot, ok2 := slotMap[item.TimeSlotID]; ok2 && contains(teacher.UnavailableSlots, slot.Code) {
				out = append(out, dto.ConflictResponse{
					Type: constants.ConflictTeacherPref, EntityType: "teacher", EntityID: item.TeacherID,
					EntityName: teacher.Name, Week: item.Week, DayOfWeek: item.DayOfWeek, TimeSlotID: item.TimeSlotID,
					Suggestion: fmt.Sprintf("slot %s is in the teacher's unavailable periods; choose another time", slot.Code),
				})
			}
		}
	}
	return out, nil
}
