package application

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"strings"
	"time"
)

func (s *Service) Execution(ctx context.Context, date, orderID, staffID, action, report string) error {
	rank := map[string]int{"DEPART": 1, "ARRIVE": 2, "START": 3, "COMPLETE": 4}
	if rank[action] == 0 {
		return invalid("Неизвестное действие.")
	}
	report = strings.TrimSpace(report)
	if len([]rune(report)) > 2000 || (action == "COMPLETE" && len([]rune(report)) < 3) {
		return invalid("Добавьте отчёт о выполнении: 3–2000 символов.")
	}
	if e := s.SyncClock(ctx, date, true); e != nil {
		return e
	}
	return s.Store.WithDayLock(ctx, date, func() error {
		in, e := s.Store.EventInputs(ctx, date)
		if e != nil {
			return planError(e)
		}
		if in.ClockMode == "LIVE" && date != s.wallNow().Format("2006-01-02") {
			return &Error{409, "LIVE_DAY_ENDED", "Рабочая дата завершена. Для действий с историей включите деморежим."}
		}
		if in.ClockMode == "LIVE" && s.wallNow().Before(c.Time(in.Day.Now)) {
			return &Error{409, "CLOCK_AHEAD", "Время смены опережает сервер. Подтверждение приостановлено до синхронизации."}
		}
		var visit *dto.Stop
		preceding := []string{}
		for _, route := range in.Plan.Routes {
			if route.StaffId != staffID {
				continue
			}
			for i := range route.Stops {
				stop := route.Stops[i]
				if stop.Type != "VISIT" || stop.OrderId == nil {
					continue
				}
				if *stop.OrderId == orderID {
					visit = &stop
					break
				}
				preceding = append(preceding, *stop.OrderId)
			}
		}
		if visit == nil {
			return &Error{403, "FORBIDDEN", "Задание не назначено этому исполнителю."}
		}
		return s.Store.Update(ctx, func(tx pgx.Tx) error {
			var generation, revision int64
			if e := tx.QueryRow(ctx, "SELECT generation,plan_revision FROM days WHERE date::text=$1", date).Scan(&generation, &revision); e != nil {
				return e
			}
			if generation != in.Generation || revision != in.Revision {
				return postgres.ErrStale
			}
			var status, owner string
			e := tx.QueryRow(ctx, "SELECT status,staff_id FROM visit_facts WHERE day=$1 AND generation=$2 AND order_id=$3", date, generation, orderID).Scan(&status, &owner)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if owner != "" && owner != staffID {
				return &Error{409, "EXECUTION_OWNER_CHANGED", "История выполнения принадлежит другому сотруднику. Обратитесь к диспетчеру."}
			}
			stateRank := map[string]int{"EN_ROUTE": 1, "ARRIVED": 2, "IN_PROGRESS": 3, "COMPLETED": 4}
			// Retries never move a job backwards or replace original timestamps/report.
			if stateRank[status] >= rank[action] {
				return nil
			}
			if action == "ARRIVE" && status != "EN_ROUTE" {
				return &Error{409, "DEPARTURE_REQUIRED", "Сначала подтвердите выезд к абоненту."}
			}
			if action == "START" && status == "EN_ROUTE" {
				return &Error{409, "ARRIVAL_REQUIRED", "Сначала подтвердите прибытие на объект."}
			}
			if action == "COMPLETE" && status != "IN_PROGRESS" {
				return &Error{409, "VISIT_NOT_STARTED", "Сначала подтвердите начало работы."}
			}
			now := c.Time(in.Day.Now)
			if (action == "START" || action == "COMPLETE") && now.Before(c.Time(visit.Start)) {
				return &Error{409, "VISIT_NOT_STARTED", "Время визита ещё не наступило. Проверьте часы смены у диспетчера."}
			}
			if action == "DEPART" {
				departure := c.Time(visit.Arrival)
				if visit.TravelSec != nil {
					departure = departure.Add(-time.Duration(*visit.TravelSec) * time.Second)
				}
				if now.Before(departure) {
					return &Error{409, "DEPARTURE_NOT_DUE", "Время выезда по маршруту ещё не наступило: " + departure.Format("15:04") + "."}
				}
				var unfinished bool
				if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM unnest($4::text[]) id WHERE NOT EXISTS(SELECT 1 FROM visit_facts f WHERE f.day=$1 AND f.generation=$2 AND f.staff_id=$3 AND f.order_id=id AND f.status='COMPLETED'))`, date, generation, staffID, preceding).Scan(&unfinished); e != nil {
					return e
				}
				if unfinished {
					return &Error{409, "PREVIOUS_VISIT_UNFINISHED", "Сначала завершите предыдущие задания маршрута."}
				}
			}
			if action == "DEPART" || action == "START" {
				var busy bool
				if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visit_facts WHERE day=$1 AND generation=$2 AND staff_id=$3 AND order_id<>$4 AND status<>'COMPLETED')", date, generation, staffID, orderID).Scan(&busy); e != nil {
					return e
				}
				if busy {
					return &Error{409, "STAFF_BUSY", "Сначала завершите текущее задание."}
				}
			}
			stamp := string(in.Day.Now)
			switch action {
			case "DEPART":
				_, e = tx.Exec(ctx, "INSERT INTO visit_facts(day,generation,order_id,staff_id,status,departed_at) VALUES($1,$2,$3,$4,'EN_ROUTE',$5)", date, generation, orderID, staffID, stamp)
			case "ARRIVE":
				_, e = tx.Exec(ctx, "UPDATE visit_facts SET status='ARRIVED',arrived_at=$4 WHERE day=$1 AND generation=$2 AND order_id=$3", date, generation, orderID, stamp)
			case "START":
				_, e = tx.Exec(ctx, "INSERT INTO visit_facts(day,generation,order_id,staff_id,status,started_at) VALUES($1,$2,$3,$4,'IN_PROGRESS',$5) ON CONFLICT(day,generation,order_id) DO UPDATE SET status='IN_PROGRESS',started_at=excluded.started_at", date, generation, orderID, staffID, stamp)
			case "COMPLETE":
				_, e = tx.Exec(ctx, "UPDATE visit_facts SET status='COMPLETED',completed_at=$4,report=$5 WHERE day=$1 AND generation=$2 AND order_id=$3", date, generation, orderID, stamp, report)
			}
			return e
		})
	})
}
