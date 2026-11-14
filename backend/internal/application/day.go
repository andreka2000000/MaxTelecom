package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"strings"
	"time"
)

// CreateDay adds a fresh operational shift. Unlike the organizer PUT endpoint,
// it never replaces an existing day, its events, or its plan generation.
func (s *Service) CreateDay(ctx context.Context, date string) (dto.DayInput, error) {
	return s.CreateDayMode(ctx, date, "DEMO")
}
func (s *Service) CreateDayMode(ctx context.Context, date, mode string) (dto.DayInput, error) {
	day := dto.DayInput{Date: date, Now: dto.DateTime(date + "T08:30:00+05:00"), Orders: []dto.Order{}, AbsentStaffIds: []string{}}
	if mode == "" {
		mode = "DEMO"
	}
	if mode != "DEMO" && mode != "LIVE" {
		return day, invalid("Выберите режим времени.")
	}
	if mode == "LIVE" {
		now := s.wallNow()
		if date != now.Format("2006-01-02") {
			return day, &Error{409, "LIVE_DATE_REQUIRED", "Рабочую смену можно создать только на сегодняшнюю дату Екатеринбурга."}
		}
		day.Now = dto.DateTime(now.Format(time.RFC3339))
	}
	if _, e := time.Parse("2006-01-02", date); e != nil {
		return day, &Error{400, "INVALID_DATE", "Выберите дату смены."}
	}
	e := s.Store.Update(ctx, func(tx pgx.Tx) error {
		var exists bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM days WHERE date::text=$1)", date).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return &Error{409, "DAY_EXISTS", "Смена на эту дату уже существует. Её данные сохранены."}
		}
		reference, e := postgres.Read[dto.ReferenceData](ctx, tx, "reference_data")
		if e != nil {
			return e
		}
		matrix, e := postgres.Read[dto.TravelMatrix](ctx, tx, "travel_matrices")
		if e != nil {
			return e
		}
		if reference == nil || matrix == nil {
			return &Error{409, "REFERENCE_NOT_LOADED", "Сначала загрузите справочники и дорожную матрицу."}
		}
		if e := postgres.ReplaceDay(ctx, tx, day); e != nil {
			return e
		}
		if mode == "LIVE" {
			_, e := tx.Exec(ctx, `UPDATE days SET clock_mode='LIVE',clock_synced_at=$2 WHERE date::text=$1`, date, s.wallNow())
			return e
		}
		return nil
	})
	return day, e
}

// CopyDay creates a fresh, dated scenario without changing the source, its
// execution facts, subscriber links, events, or published plan.
func (s *Service) CopyDay(ctx context.Context, source, target string) (dto.DayInput, error) {
	var day dto.DayInput
	from, e := time.Parse("2006-01-02", source)
	to, targetErr := time.Parse("2006-01-02", target)
	if e != nil || targetErr != nil || source == target {
		return day, &Error{400, "INVALID_DATE", "Выберите новую дату, отличную от исходной смены."}
	}
	e = s.Store.WithDayLock(ctx, target, func() error {
		data, e := s.Store.Workspace(ctx, source)
		if errors.Is(e, postgres.ErrMissing) {
			return &Error{404, "DAY_NOT_FOUND", "Исходная смена не найдена."}
		}
		if e != nil {
			return e
		}
		offset := int(to.Sub(from).Hours() / 24)
		shift := func(value dto.DateTime) dto.DateTime {
			v, _ := time.Parse(time.RFC3339, string(value))
			return dto.DateTime(v.AddDate(0, 0, offset).Format(time.RFC3339))
		}
		day = data.Day
		day.Date, day.Now = target, shift(day.Now)
		day.Orders = append([]dto.Order{}, data.Day.Orders...)
		ids := map[string]string{}
		for i, o := range day.Orders {
			ids[o.OrderId] = fmt.Sprintf("ORD-%s-%04d", strings.ReplaceAll(target, "-", ""), i+1)
		}
		for i := range day.Orders {
			o := &day.Orders[i]
			o.OrderId = ids[o.OrderId]
			o.RegisteredAt, o.LatestStart, o.Deadline = shift(o.RegisteredAt), shift(o.LatestStart), shift(o.Deadline)
			o.Slot = dto.TimeWindow{Start: shift(o.Slot.Start), End: shift(o.Slot.End)}
			if o.PreviousOrderId != nil {
				if mapped, ok := ids[*o.PreviousOrderId]; ok {
					o.PreviousOrderId = &mapped
				}
			}
		}
		if e = ValidateDay(target, &day, data.Reference); e != nil {
			return e
		}
		return s.Store.Update(ctx, func(tx pgx.Tx) error {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM days WHERE date::text=$1)", target).Scan(&exists); e != nil {
				return e
			}
			if exists {
				return &Error{409, "DAY_EXISTS", "Смена на эту дату уже существует. Выберите свободную дату."}
			}
			if e := postgres.ReplaceDay(ctx, tx, day); e != nil {
				return e
			}
			overrides := map[string]dto.Staff{}
			for _, person := range data.Reference.Staff {
				overrides[person.StaffId] = person
			}
			raw, e := json.Marshal(overrides)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, "UPDATE days SET source_date=$2,staff_overrides=$3 WHERE date::text=$1", target, source, raw)
			return e
		})
	})
	return day, e
}
