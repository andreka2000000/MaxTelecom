package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"magistral/backend/internal/auth"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/insights"
	"magistral/backend/internal/planning"
	"magistral/backend/internal/storage/postgres"
	"sort"
	"strings"
	"time"
)

type AssignmentOption struct {
	StaffID        string                 `json:"staff_id"`
	Name           string                 `json:"name"`
	Available      bool                   `json:"available"`
	Code           string                 `json:"code"`
	Reason         string                 `json:"reason"`
	Start          string                 `json:"start,omitempty"`
	End            string                 `json:"end,omitempty"`
	TravelDeltaSec int64                  `json:"travel_delta_sec"`
	Changes        []planning.VisitChange `json:"changes"`
	Token          string                 `json:"token,omitempty"`
	Plan           *dto.Plan              `json:"-"`
}
type AssignmentPreview struct {
	Generation   int64              `json:"generation"`
	PlanVersion  int64              `json:"plan_version"`
	OrderID      string             `json:"order_id"`
	CurrentStaff string             `json:"current_staff,omitempty"`
	Allowed      bool               `json:"allowed"`
	Reason       string             `json:"reason,omitempty"`
	Options      []AssignmentOption `json:"options"`
}
type AssignmentInput struct {
	RequestID string `json:"request_id"`
	StaffID   string `json:"staff_id"`
	Token     string `json:"preview_token"`
	Reason    string `json:"reason"`
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func assignmentWorld(in postgres.EventInputs) (*c.World, error) {
	w, e := c.New(in.Reference, in.Day, in.Matrix)
	if e != nil {
		return nil, e
	}
	w.Previous = &in.Plan
	w.Groups = in.Plan.PgpGroups
	w.EndOverrides = in.State.EndOverrides
	w.NewOrderAt = in.State.NewOrderAt
	w.Unavailable = in.State.Unavailable
	return w, nil
}
func (s *Service) assignmentContext(ctx context.Context, date, id string) (postgres.EventInputs, *c.World, AssignmentPreview, error) {
	out := AssignmentPreview{OrderID: id, Options: []AssignmentOption{}}
	in, e := s.Store.EventInputs(ctx, date)
	if e != nil {
		if errors.Is(e, postgres.ErrNoPlan) {
			return in, nil, out, &Error{409, "PLAN_NOT_BUILT", "Сначала постройте первоначальный план."}
		}
		return in, nil, out, planError(e)
	}
	out.Generation = in.Generation
	out.PlanVersion = in.Plan.PlanVersion
	w, e := assignmentWorld(in)
	if e != nil {
		return in, w, out, e
	}
	order, ok := w.Orders[id]
	if !ok {
		return in, w, out, &Error{404, "ORDER_NOT_FOUND", "Наряд не найден в выбранной смене."}
	}
	if !WorkMatchesBuilding(order.WorkTypeCode, order.Technology, order.BuildingType) {
		out.Reason = "В исходном наряде вид работ не соответствует технологии или типу здания. Сначала исправьте исходные данные."
		return in, w, out, nil
	}
	for _, g := range in.Plan.PgpGroups {
		for _, o := range g.OrderIds {
			if o == id {
				out.Reason = "Наряд передан на станционную диагностику ПГП."
				return in, w, out, nil
			}
		}
	}
	for _, o := range in.Plan.Cancelled {
		if o.OrderId == id {
			out.Reason = "Отменённый наряд нельзя назначить."
			return in, w, out, nil
		}
	}
	if in.State.Transferred[id] {
		out.Reason = "Визит уже передан в подразделение."
		return in, w, out, nil
	}
	for _, r := range in.Plan.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == id {
				out.CurrentStaff = r.StaffId
				if !c.Time(stop.Start).After(c.Time(in.Day.Now)) {
					out.Reason = "Время начала визита уже наступило. История назначения зафиксирована."
					return in, w, out, nil
				}
			}
		}
	}
	var hasFact bool
	if e = s.Store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visit_facts WHERE day=$1 AND generation=$2 AND order_id=$3)", date, in.Generation, id).Scan(&hasFact); e != nil {
		return in, w, out, e
	}
	if hasFact {
		out.Reason = "Работа уже начата или завершена исполнителем."
		return in, w, out, nil
	}
	out.Allowed = true
	return in, w, out, nil
}
func assignmentOption(ctx context.Context, in postgres.EventInputs, w *c.World, id, current string, person dto.Staff) AssignmentOption {
	o := w.Orders[id]
	work := w.Work[o.WorkTypeCode]
	opt := AssignmentOption{StaffID: person.StaffId, Name: person.StaffId, Changes: []planning.VisitChange{}}
	if person.DisplayName != nil {
		opt.Name = *person.DisplayName
	}
	reject := func(code, reason string) AssignmentOption { opt.Code = code; opt.Reason = reason; return opt }
	if person.StaffId == current {
		return reject("CURRENT", "Текущий исполнитель.")
	}
	if c.Has(in.Day.AbsentStaffIds, person.StaffId) {
		return reject("ABSENT", "Сотрудник отсутствует в этой смене.")
	}
	profile := false
	for _, p := range person.Profiles {
		if c.Has(work.Profiles, p) {
			profile = true
		}
	}
	if !profile {
		return reject("PROFILE", "Не подходит специализация сотрудника.")
	}
	if !c.Has(person.Technologies, o.Technology) {
		return reject("TECHNOLOGY", "Нет допуска к технологии "+string(o.Technology)+".")
	}
	if o.RequiresHeight && !person.HeightPermit {
		return reject("HEIGHT", "Нет допуска к высотным работам.")
	}
	if !c.Enough(person.EquipmentCapacity, o.RequiredEquipment) {
		return reject("EQUIPMENT", "Требуемое оборудование превышает вместимость сотрудника.")
	}
	for _, u := range w.Unavailable[person.StaffId] {
		if u.Until == nil {
			return reject("UNAVAILABLE", "Сотрудник недоступен до конца смены.")
		}
	}
	if c.Time(in.Day.Now).After(c.Time(o.LatestStart)) {
		return reject("WINDOW", "Допустимое время начала визита уже истекло.")
	}
	p, e := planning.Assign(ctx, w, in.Plan, id, person.StaffId)
	if e != nil {
		return reject("SCHEDULE", e.Error())
	}
	insights.Alerts(w, in.Plan.Alerts, insights.Forecast(w, in.Plan, nil), &p)
	if e = w.Validate(p); e != nil {
		return reject("CONSTRAINT", e.Error())
	}
	opt.Available = true
	opt.Code = "AVAILABLE"
	opt.Reason = "Назначение допустимо; остальные наряды сохраняют исполнителей."
	for _, r := range p.Routes {
		if r.StaffId == person.StaffId {
			for _, s := range r.Stops {
				if s.OrderId != nil && *s.OrderId == id {
					opt.Start = string(s.Start)
					opt.End = string(s.End)
				}
			}
		}
	}
	travel := func(plan dto.Plan) int64 {
		var total int64
		for _, route := range plan.Routes {
			for _, stop := range route.Stops {
				if stop.TravelSec != nil {
					total += *stop.TravelSec
				}
			}
		}
		return total
	}
	opt.TravelDeltaSec = travel(p) - travel(in.Plan)
	opt.Changes = planning.VisitChanges(in.Plan, p)
	opt.Token = digest(struct {
		Generation, Revision, Reference, Matrix int64
		Order, Staff                            string
		Routes                                  []dto.Route
	}{in.Generation, in.Revision, in.ReferenceVersion, in.MatrixVersion, id, person.StaffId, p.Routes})
	opt.Plan = &p
	return opt
}
func (s *Service) AssignmentPreview(ctx context.Context, date, id string) (AssignmentPreview, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	in, w, out, e := s.assignmentContext(ctx, date, id)
	if e != nil || !out.Allowed {
		return out, e
	}
	staff := append([]dto.Staff{}, in.Reference.Staff...)
	sort.Slice(staff, func(i, j int) bool { return staff[i].StaffId < staff[j].StaffId })
	for _, person := range staff {
		out.Options = append(out.Options, assignmentOption(ctx, in, w, id, out.CurrentStaff, person))
		if ctx.Err() != nil {
			return out, &Error{503, "ASSIGNMENT_TIMEOUT", "Проверка не завершена. Повторите расчёт."}
		}
	}
	return out, nil
}
func (s *Service) Assign(ctx context.Context, date, id string, input AssignmentInput, actor auth.User) (postgres.AssignmentRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var result postgres.AssignmentRecord
	input.Reason = strings.TrimSpace(input.Reason)
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 200 || len(input.Token) != 64 || len([]rune(input.Reason)) < 3 || len([]rune(input.Reason)) > 2000 {
		return result, invalid("Укажите актуальный предпросмотр, идентификатор и причину назначения (3–2000 символов).")
	}
	hash := digest(struct {
		Order, Actor string
		Input        AssignmentInput
	}{id, actor.ID, input})
	e := s.Store.WithDayLock(ctx, date, func() error {
		duplicate, oldHash, e := s.Store.AssignmentDuplicate(ctx, date, input.RequestID)
		if e != nil {
			return e
		}
		if oldHash != "" {
			if oldHash != hash {
				return &Error{409, "IDEMPOTENCY_CONFLICT", "Идентификатор уже использован для другого назначения."}
			}
			result = duplicate
			return nil
		}
		in, w, out, e := s.assignmentContext(ctx, date, id)
		if e != nil {
			return e
		}
		if !out.Allowed {
			return &Error{409, "ASSIGNMENT_LOCKED", out.Reason}
		}
		person, ok := w.Staff[input.StaffID]
		if !ok {
			return invalid("Выберите сотрудника этой смены.")
		}
		opt := assignmentOption(ctx, in, w, id, out.CurrentStaff, person)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !opt.Available {
			return &Error{422, "ASSIGNMENT_INFEASIBLE", opt.Reason}
		}
		if opt.Token != input.Token {
			return &Error{409, "ASSIGNMENT_STALE", "План изменился. Обновите предпросмотр и подтвердите новое назначение."}
		}
		result = postgres.AssignmentRecord{RequestID: input.RequestID, OrderID: id, ActorID: actor.ID, ActorName: actor.Name, FromStaff: out.CurrentStaff, ToStaff: input.StaffID, Reason: input.Reason, AsOf: string(in.Day.Now), PlanVersion: opt.Plan.PlanVersion}
		return planError(s.Store.PublishAssignment(ctx, in, *opt.Plan, result, hash))
	})
	if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		e = &Error{503, "ASSIGNMENT_TIMEOUT", fmt.Sprint("Назначение не подтверждено. Повторите с тем же идентификатором: ", input.RequestID)}
	}
	return result, e
}
