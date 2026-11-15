package application

import (
	"context"
	"errors"
	"magistral/backend/internal/auth"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/insights"
	"magistral/backend/internal/planning"
	"magistral/backend/internal/storage/postgres"
	"reflect"
	"sort"
	"strings"
	"time"
)

type StaffSettings struct {
	Name              string           `json:"name"`
	Profiles          []dto.Profile    `json:"profiles"`
	Technologies      []dto.Technology `json:"technologies"`
	HeightPermit      bool             `json:"height_permit"`
	Shift             dto.Shift        `json:"shift"`
	EquipmentStart    dto.Equipment    `json:"equipment_start"`
	EquipmentCapacity dto.Equipment    `json:"equipment_capacity"`
	Absent            bool             `json:"absent"`
}
type StaffEditInput struct {
	Generation int64         `json:"generation"`
	Revision   int64         `json:"revision"`
	Settings   StaffSettings `json:"settings"`
	RequestID  string        `json:"request_id"`
	Token      string        `json:"preview_token"`
	Reason     string        `json:"reason"`
}
type StaffEditPreview struct {
	Generation       int64                       `json:"generation"`
	Revision         int64                       `json:"revision"`
	PlanVersion      int64                       `json:"plan_version"`
	HasPlan          bool                        `json:"has_plan"`
	Token            string                      `json:"token"`
	Before           dto.Staff                   `json:"before"`
	After            dto.Staff                   `json:"after"`
	AbsentBefore     bool                        `json:"absent_before"`
	AbsentAfter      bool                        `json:"absent_after"`
	Changes          []postgres.StaffVisitChange `json:"changes"`
	VisitsBefore     int                         `json:"visits_before"`
	VisitsAfter      int                         `json:"visits_after"`
	UnassignedBefore int                         `json:"unassigned_before"`
	UnassignedAfter  int                         `json:"unassigned_after"`
	TravelDeltaSec   int64                       `json:"travel_delta_sec"`
	Plan             *dto.Plan                   `json:"-"`
	Day              dto.DayInput                `json:"-"`
	Inputs           postgres.EventInputs        `json:"-"`
}

func validateStaffSettings(s *StaffSettings) error {
	s.Name = strings.TrimSpace(s.Name)
	if len([]rune(s.Name)) < 1 || len([]rune(s.Name)) > 160 {
		return invalid("Имя сотрудника: от 1 до 160 символов.")
	}
	if len(s.Profiles) == 0 || len(s.Technologies) == 0 {
		return invalid("Выберите хотя бы одну специализацию и технологию.")
	}
	profiles := map[dto.Profile]bool{}
	for _, p := range s.Profiles {
		if (p != "LTP3" && p != "INSTALL") || profiles[p] {
			return invalid("Недопустимая или повторная специализация.")
		}
		profiles[p] = true
	}
	tech := map[dto.Technology]bool{}
	for _, v := range s.Technologies {
		if !c.Has([]dto.Technology{"PON", "FTTB", "XDSL", "DOCSIS"}, v) || tech[v] {
			return invalid("Недопустимая или повторная технология.")
		}
		tech[v] = true
	}
	s.Profiles = append([]dto.Profile{}, s.Profiles...)
	s.Technologies = append([]dto.Technology{}, s.Technologies...)
	sort.Slice(s.Profiles, func(i, j int) bool { return s.Profiles[i] < s.Profiles[j] })
	sort.Slice(s.Technologies, func(i, j int) bool { return s.Technologies[i] < s.Technologies[j] })
	sh := s.Shift
	for _, v := range []dto.LocalTime{sh.Start, sh.End, sh.BreakWindow.From, sh.BreakWindow.To} {
		t, e := time.Parse("15:04", string(v))
		if e != nil || t.Format("15:04") != string(v) {
			return invalid("Время смены и обеда указывается в формате ЧЧ:ММ.")
		}
	}
	from, _ := time.Parse("15:04", string(sh.BreakWindow.From))
	end, _ := time.Parse("15:04", string(sh.End))
	if sh.Start >= sh.End || sh.BreakWindow.From < sh.Start || sh.BreakWindow.To > sh.End || sh.BreakWindow.From > sh.BreakWindow.To || sh.BreakDurationMin < 0 || sh.BreakDurationMin > 1440 || from.Add(time.Duration(sh.BreakDurationMin)*time.Minute).After(end) {
		return invalid("Проверьте начало и конец смены: окно начала обеда и его длительность должны помещаться в смену.")
	}
	for _, eq := range []dto.Equipment{s.EquipmentStart, s.EquipmentCapacity} {
		for _, n := range []int64{eq.ONT, eq.ROUTER, eq.STB} {
			if n < 0 || n > 1000000 {
				return invalid("Оборудование: целые количества от 0 до 1 000 000.")
			}
		}
	}
	if !c.Enough(s.EquipmentCapacity, s.EquipmentStart) {
		return invalid("Начальный запас оборудования не может превышать вместимость.")
	}
	return nil
}
func staffSettings(person dto.Staff, absent bool) StaffSettings {
	name := person.StaffId
	if person.DisplayName != nil {
		name = *person.DisplayName
	}
	return StaffSettings{name, person.Profiles, person.Technologies, person.HeightPermit, person.Shift, person.EquipmentStart, person.EquipmentCapacity, absent}
}

func staffVisitChanges(before, after dto.Plan) ([]postgres.StaffVisitChange, int, int) {
	type visit struct {
		staff string
		stop  dto.Stop
	}
	visits := func(p dto.Plan) map[string]visit {
		out := map[string]visit{}
		for _, r := range p.Routes {
			for _, s := range r.Stops {
				if s.Type == "VISIT" && s.OrderId != nil {
					out[*s.OrderId] = visit{r.StaffId, s}
				}
			}
		}
		return out
	}
	a, b := visits(before), visits(after)
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	reasons := map[string]string{}
	for _, u := range after.Unassigned {
		reasons[u.OrderId] = u.ReasonText
	}
	out := []postgres.StaffVisitChange{}
	for id := range ids {
		old, now := a[id], b[id]
		if old.staff == now.staff && old.stop.Start == now.stop.Start && old.stop.End == now.stop.End {
			continue
		}
		out = append(out, postgres.StaffVisitChange{OrderID: id, BeforeStaff: old.staff, AfterStaff: now.staff, BeforeStart: string(old.stop.Start), BeforeEnd: string(old.stop.End), AfterStart: string(now.stop.Start), AfterEnd: string(now.stop.End), Reason: reasons[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderID < out[j].OrderID })
	return out, len(a), len(b)
}
func staffPreviewToken(in postgres.EventInputs, id string, settings StaffSettings, p *dto.Plan) string {
	// Input versions invalidate the preview even if the resulting routes match.
	var routes []dto.Route
	var unassigned []dto.UnassignedOrder
	if p != nil {
		routes = p.Routes
		unassigned = p.Unassigned
	}
	return digest(struct {
		Generation, Revision, Reference, Matrix int64
		Staff                                   string
		Settings                                StaffSettings
		Routes                                  []dto.Route
		Unassigned                              []dto.UnassignedOrder
	}{in.Generation, in.Revision, in.ReferenceVersion, in.MatrixVersion, id, settings, routes, unassigned})
}
func (s *Service) previewStaff(ctx context.Context, date, id string, input StaffEditInput) (StaffEditPreview, error) {
	var out StaffEditPreview
	settings := input.Settings
	if e := validateStaffSettings(&settings); e != nil {
		return out, e
	}
	in, e := s.Store.EditInputs(ctx, date)
	if e != nil {
		return out, planError(e)
	}
	if input.Generation != in.Generation || input.Revision != in.Revision {
		return out, &Error{409, "STAFF_EDIT_STALE", "Данные или план изменились. Закройте редактор, обновите смену и откройте актуальную карточку."}
	}
	index := -1
	for i, p := range in.Reference.Staff {
		if p.StaffId == id {
			index = i
			break
		}
	}
	if index < 0 {
		return out, &Error{404, "STAFF_NOT_FOUND", "Сотрудник не найден."}
	}
	before := in.Reference.Staff[index]
	oldSettings := staffSettings(before, c.Has(in.Day.AbsentStaffIds, id))
	if e = validateStaffSettings(&oldSettings); e != nil {
		return out, e
	}
	if reflect.DeepEqual(oldSettings, settings) {
		return out, invalid("Карточка не изменена. Сначала измените настройки.")
	}
	for _, r := range in.Plan.Routes {
		if r.StaffId == id {
			for _, stop := range r.Stops {
				if !c.Time(stop.Start).After(c.Time(in.Day.Now)) {
					return out, &Error{409, "STAFF_HISTORY_LOCKED", "Маршрут сотрудника уже начался. Настройки этой смены зафиксированы. Для выбытия в течение дня используйте событие «Недоступность мастера»."}
				}
			}
		}
	}
	var hasFact bool
	if e = s.Store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visit_facts WHERE day=$1 AND generation=$2 AND staff_id=$3)", date, in.Generation, id).Scan(&hasFact); e != nil {
		return out, e
	}
	if hasFact {
		return out, &Error{409, "STAFF_HISTORY_LOCKED", "У сотрудника есть факты выполнения. Изменения не должны переписывать историю."}
	}
	after := before
	after.DisplayName = &settings.Name
	after.Profiles = settings.Profiles
	after.Technologies = settings.Technologies
	after.HeightPermit = settings.HeightPermit
	after.Shift = settings.Shift
	after.EquipmentStart = settings.EquipmentStart
	after.EquipmentCapacity = settings.EquipmentCapacity
	reference := in.Reference
	reference.Staff = append([]dto.Staff{}, in.Reference.Staff...)
	reference.Staff[index] = after
	if e = ValidateReference(reference); e != nil {
		return out, invalid("Проверьте настройки сотрудника: %s", e)
	}
	day := in.Day
	day.AbsentStaffIds = []string{}
	for _, staff := range in.Day.AbsentStaffIds {
		if staff != id {
			day.AbsentStaffIds = append(day.AbsentStaffIds, staff)
		}
	}
	if settings.Absent {
		day.AbsentStaffIds = append(day.AbsentStaffIds, id)
	}
	sort.Strings(day.AbsentStaffIds)
	out = StaffEditPreview{Generation: in.Generation, Revision: in.Revision, PlanVersion: in.Plan.PlanVersion, HasPlan: in.Plan.PlanVersion > 0, Before: before, After: after, AbsentBefore: oldSettings.Absent, AbsentAfter: settings.Absent, Changes: []postgres.StaffVisitChange{}, Day: day, Inputs: in}
	if out.HasPlan {
		w, e := c.New(reference, day, in.Matrix)
		if e != nil {
			return out, e
		}
		w.Previous = &in.Plan
		w.Groups = in.Plan.PgpGroups
		w.Unavailable = in.State.Unavailable
		w.NewOrderAt = in.State.NewOrderAt
		w.EndOverrides = in.State.EndOverrides
		history, e := s.executionHistory(ctx, date, in.Generation)
		if e != nil {
			return out, e
		}
		w.Pinned = history.Pins
		seed := in.Plan
		seed.Routes = []dto.Route{}
		present := map[string]bool{}
		pinned := history.Pins
		for _, r := range in.Plan.Routes {
			for _, stop := range r.Stops {
				if stop.OrderId != nil && !c.Time(stop.Start).After(c.Time(day.Now)) {
					pinned[*stop.OrderId] = true
				}
			}
			if !c.Has(day.AbsentStaffIds, r.StaffId) {
				seed.Routes = append(seed.Routes, r)
				present[r.StaffId] = true
			}
		}
		for _, person := range reference.Staff {
			if !c.Has(day.AbsentStaffIds, person.StaffId) && !present[person.StaffId] {
				seed.Routes = append(seed.Routes, dto.Route{StaffId: person.StaffId, ServiceCenterId: person.ServiceCenterId, Status: "ACTIVE", Stops: []dto.Stop{}})
			}
		}
		cancelled := map[string]dto.CancelledOrder{}
		for _, o := range in.Plan.Cancelled {
			cancelled[o.OrderId] = o
		}
		p, e := planning.Repair(ctx, w, seed, cancelled, pinned)
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if e != nil {
			return out, &Error{422, "STAFF_EDIT_INFEASIBLE", "Не удалось сохранить допустимый план с неизменной историей. Измените параметры смены или используйте событие недоступности."}
		}
		p.BasedOnEventId = nil
		p.Optimization = nil
		insights.Alerts(w, in.Plan.Alerts, insights.Forecast(w, in.Plan, cancelled), &p)
		if e = w.Validate(p); e != nil {
			return out, &Error{422, "STAFF_EDIT_INFEASIBLE", "Изменение нарушает ограничения сохранённого плана."}
		}
		out.Changes, out.VisitsBefore, out.VisitsAfter = staffVisitChanges(in.Plan, p)
		out.UnassignedBefore = len(in.Plan.Unassigned)
		out.UnassignedAfter = len(p.Unassigned)
		travel := func(p dto.Plan) int64 {
			var n int64
			for _, r := range p.Routes {
				for _, stop := range r.Stops {
					if stop.TravelSec != nil {
						n += *stop.TravelSec
					}
				}
			}
			return n
		}
		out.TravelDeltaSec = travel(p) - travel(in.Plan)
		out.Plan = &p
	}
	out.Token = staffPreviewToken(in, id, settings, out.Plan)
	return out, nil
}
func staffEditError(e error) error {
	if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		return &Error{503, "STAFF_EDIT_TIMEOUT", "Расчёт не подтвердился вовремя. Повторите запрос; идентификатор защищает от дублирования."}
	}
	return e
}
func (s *Service) PreviewStaff(ctx context.Context, date, id string, input StaffEditInput) (StaffEditPreview, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, e := s.previewStaff(ctx, date, id, input)
	return out, staffEditError(e)
}
func (s *Service) EditStaff(ctx context.Context, date, id string, input StaffEditInput, actor auth.User) (postgres.StaffChangeRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var result postgres.StaffChangeRecord
	input.Reason = strings.TrimSpace(input.Reason)
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 200 || len(input.Token) != 64 || len([]rune(input.Reason)) < 3 || len([]rune(input.Reason)) > 2000 {
		return result, invalid("Укажите предпросмотр, идентификатор и причину изменения (3–2000 символов).")
	}
	if e := validateStaffSettings(&input.Settings); e != nil {
		return result, e
	}
	hash := digest(struct {
		Staff, Actor string
		Input        StaffEditInput
	}{id, actor.ID, input})
	e := s.Store.WithDayLock(ctx, date, func() error {
		duplicate, oldHash, e := s.Store.StaffChangeDuplicate(ctx, date, input.RequestID)
		if e != nil {
			return e
		}
		if oldHash != "" {
			if oldHash != hash {
				return &Error{409, "IDEMPOTENCY_CONFLICT", "Идентификатор уже использован для другого изменения."}
			}
			result = duplicate
			return nil
		}
		preview, e := s.previewStaff(ctx, date, id, input)
		if e != nil {
			return e
		}
		if preview.Token != input.Token {
			return &Error{409, "STAFF_EDIT_STALE", "Данные или план изменились. Обновите предпросмотр и проверьте последствия."}
		}
		version := preview.PlanVersion
		if preview.Plan != nil {
			version = preview.Plan.PlanVersion
		}
		result = postgres.StaffChangeRecord{RequestID: input.RequestID, StaffID: id, ActorID: actor.ID, ActorName: actor.Name, Reason: input.Reason, AsOf: string(preview.Day.Now), PlanVersion: version, Revision: preview.Revision + 1, Before: preview.Before, After: preview.After, AbsentBefore: preview.AbsentBefore, AbsentAfter: preview.AbsentAfter, Changes: preview.Changes}
		return planError(s.Store.PublishStaffChange(ctx, preview.Inputs, preview.After, preview.Day, preview.Plan, result, hash))
	})
	return result, staffEditError(e)
}
