package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"magistral/backend/internal/auth"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/domain"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"strings"
	"time"
)

func (s *Service) wallNow() time.Time {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return now.In(domain.Location).Truncate(time.Second)
}
func (s *Service) Clock(ctx context.Context, date string) (postgres.ShiftClock, error) {
	out, e := s.Store.Clock(ctx, date)
	if e != nil {
		return out, planError(e)
	}
	now := s.wallNow()
	out.ServerNow = now.Format(time.RFC3339)
	out.State = "PAUSED"
	if out.Mode == "LIVE" {
		out.State = "RUNNING"
		if out.Date < now.Format("2006-01-02") {
			out.State = "ENDED"
		}
		if c.Time(dto.DateTime(out.ModelNow)).After(now) {
			out.State = "AHEAD"
		}
		if out.Error != "" {
			out.State = "BLOCKED"
		}
		if out.State == "RUNNING" && now.Sub(c.Time(dto.DateTime(out.ModelNow))) > 90*time.Second {
			out.State = "LAGGING"
		}
	}
	return out, nil
}

type ClockModeInput struct {
	Mode       string `json:"mode"`
	Generation int64  `json:"generation"`
	Revision   int64  `json:"revision"`
	RequestID  string `json:"request_id"`
}

func (s *Service) SetClockMode(ctx context.Context, date string, input ClockModeInput, actor auth.User) (postgres.ClockChange, error) {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	var out postgres.ClockChange
	if (input.Mode != "LIVE" && input.Mode != "DEMO") || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 160 {
		return out, invalid("Выберите режим и идентификатор изменения.")
	}
	hash := digest(struct {
		Input ClockModeInput
		Actor string
	}{input, actor.ID})
	e := s.Store.WithDayLock(ctx, date, func() error {
		saved, oldHash, e := s.Store.ClockDuplicate(ctx, date, input.RequestID)
		if e != nil {
			return e
		}
		if oldHash != "" {
			if hash != oldHash {
				return &Error{409, "IDEMPOTENCY_CONFLICT", "Идентификатор уже использован для другого переключения."}
			}
			out = saved
			return nil
		}
		clock, e := s.Store.Clock(ctx, date)
		if e != nil {
			return planError(e)
		}
		if clock.Generation != input.Generation || clock.Revision != input.Revision {
			return &Error{409, "CLOCK_CHANGED", "Режим уже изменился. Обновите данные часов."}
		}
		if clock.Mode == input.Mode {
			return &Error{409, "CLOCK_UNCHANGED", "Этот режим уже включён."}
		}
		now := s.wallNow()
		at := c.Time(dto.DateTime(clock.ModelNow))
		if input.Mode == "LIVE" {
			if date != now.Format("2006-01-02") {
				return &Error{409, "LIVE_DATE_REQUIRED", "Рабочий режим доступен только для сегодняшней даты Екатеринбурга. Создайте сегодняшнюю смену в рабочем режиме."}
			}
			if at.After(now) {
				return &Error{409, "CLOCK_AHEAD", "Часы смены опережают текущее время. Перевод назад запрещён; используйте деморежим."}
			}
			at = now
		} else if date == now.Format("2006-01-02") && now.After(at) {
			at = now
		} else if date < now.Format("2006-01-02") {
			at = c.Time(dto.DateTime(date + "T23:59:59+05:00"))
		}
		out = postgres.ClockChange{Revision: clock.Revision + 1, RequestID: input.RequestID, ActorID: actor.ID, ActorName: actor.Name, From: clock.Mode, To: input.Mode, AsOf: string(c.Stamp(at)), ChangedAt: now.Format(time.RFC3339)}
		hook := func(tx pgx.Tx) error { return postgres.WriteClockMode(ctx, tx, date, clock.Generation, out, hash) }
		in, e := s.Store.EditInputs(ctx, date)
		if e != nil {
			return planError(e)
		}
		if in.Plan.PlanVersion == 0 {
			return planError(s.Store.PublishClockWithoutPlan(ctx, in, c.Stamp(at), now, hook))
		}
		_, e = s.event(ctx, date, dto.EventInput{EventId: fmt.Sprintf("clock-mode:%d:%s", clock.Generation, input.RequestID), Type: "TIME_TICK", Timestamp: c.Stamp(at)}, "", &eventExtension{clockSync: true, alreadyLocked: true, commit: func(tx pgx.Tx, _ postgres.EventInputs, _ dto.EventResult) error { return hook(tx) }})
		return e
	})
	return out, e
}

// Automatic updates use whole minutes; execution and explicit sync use seconds.
func (s *Service) SyncClock(ctx context.Context, date string, exact bool) error {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	clock, e := s.Store.Clock(ctx, date)
	if e != nil {
		return planError(e)
	}
	if clock.Mode != "LIVE" {
		return nil
	}
	e = s.Store.WithDayLock(ctx, date, func() error {
		clock, e := s.Store.Clock(ctx, date)
		if e != nil {
			return planError(e)
		}
		if clock.Mode != "LIVE" {
			return nil
		}
		now := s.wallNow()
		today := now.Format("2006-01-02")
		if date > today {
			return nil
		}
		at := now
		if !exact {
			at = at.Truncate(time.Minute)
		}
		if date < today {
			at = c.Time(dto.DateTime(date + "T23:59:59+05:00"))
		}
		if !at.After(c.Time(dto.DateTime(clock.ModelNow))) {
			return nil
		}
		in, e := s.Store.EditInputs(ctx, date)
		if e != nil {
			return planError(e)
		}
		if in.Plan.PlanVersion == 0 {
			return planError(s.Store.PublishClockWithoutPlan(ctx, in, c.Stamp(at), now, nil))
		}
		_, e = s.event(ctx, date, dto.EventInput{EventId: fmt.Sprintf("clock:%d:%s", clock.Generation, at.Format(time.RFC3339)), Type: "TIME_TICK", Timestamp: c.Stamp(at)}, "", &eventExtension{clockSync: true, alreadyLocked: true, commit: func(tx pgx.Tx, _ postgres.EventInputs, _ dto.EventResult) error {
			_, e := tx.Exec(ctx, `UPDATE days SET clock_error='',clock_synced_at=$2 WHERE date::text=$1`, date, now)
			return e
		}})
		return e
	})
	if e != nil && !errors.Is(e, context.Canceled) {
		recordCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = s.Store.Update(recordCtx, func(tx pgx.Tx) error {
			_, err := tx.Exec(recordCtx, `UPDATE days SET clock_error=$3 WHERE date::text=$1 AND generation=$2 AND clock_mode='LIVE'`, date, clock.Generation, "Не удалось обновить время. Сохранён последний подтверждённый план; повторите синхронизацию.")
			return err
		})
	}
	return e
}
func (s *Service) RunClocks(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		dates, e := s.Store.LiveClockDays(ctx, s.wallNow().Format("2006-01-02"))
		if e != nil && ctx.Err() == nil {
			slog.Error("clock scheduler read failed", "error", e)
		}
		for _, date := range dates {
			if ctx.Err() != nil {
				return
			}
			if e = s.SyncClock(ctx, date, false); e != nil && ctx.Err() == nil {
				slog.Error("clock sync failed", "date", date, "error", e)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
