package planning

import (
	"context"
	"fmt"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"sort"
)

type VisitChange struct {
	OrderID     string `json:"order_id"`
	BeforeStaff string `json:"before_staff,omitempty"`
	AfterStaff  string `json:"after_staff"`
	BeforeStart string `json:"before_start,omitempty"`
	AfterStart  string `json:"after_start"`
	BeforeEnd   string `json:"before_end,omitempty"`
	AfterEnd    string `json:"after_end"`
}

func VisitChanges(before, after dto.Plan) []VisitChange {
	type visit struct {
		staff string
		stop  dto.Stop
	}
	old := map[string]visit{}
	for _, r := range before.Routes {
		for _, s := range r.Stops {
			if s.Type == "VISIT" && s.OrderId != nil {
				old[*s.OrderId] = visit{r.StaffId, s}
			}
		}
	}
	changes := []VisitChange{}
	for _, r := range after.Routes {
		for _, s := range r.Stops {
			if s.Type != "VISIT" || s.OrderId == nil {
				continue
			}
			prev, ok := old[*s.OrderId]
			if !ok || prev.staff != r.StaffId || prev.stop.Start != s.Start || prev.stop.End != s.End {
				changes = append(changes, VisitChange{*s.OrderId, prev.staff, r.StaffId, string(prev.stop.Start), string(s.Start), string(prev.stop.End), string(s.End)})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].OrderID < changes[j].OrderID })
	return changes
}

// Assign keeps every other assigned job and every unaffected route. It searches
// all insertion positions, preserving the relative order of the other visits.
// It never drops other work to make a manual choice appear feasible.
func Assign(ctx context.Context, w *c.World, old dto.Plan, orderID, target string) (dto.Plan, error) {
	source := ""
	for _, r := range old.Routes {
		for _, s := range r.Stops {
			if s.OrderId != nil && *s.OrderId == orderID {
				source = r.StaffId
			}
		}
	}
	p := old
	p.Routes = append([]dto.Route{}, old.Routes...)
	for i := range p.Routes {
		p.Routes[i].Stops = append([]dto.Stop{}, p.Routes[i].Stops...)
	}
	p.Unassigned = append([]dto.UnassignedOrder{}, old.Unassigned...)
	p.PlanVersion++
	p.AsOf = w.Day.Now
	p.Optimization = nil
	p.BasedOnEventId = nil
	var targetIndex = -1
	sequences := map[string][]string{}
	prefixes := map[string][]dto.Stop{}
	for i, r := range old.Routes {
		if r.StaffId == target {
			targetIndex = i
		}
		if r.StaffId != source && r.StaffId != target {
			continue
		}
		limit := 0
		for j, s := range r.Stops {
			if !c.Time(s.Start).After(c.Time(w.Day.Now)) {
				limit = j + 1
			}
		}
		prefix := append([]dto.Stop{}, r.Stops[:limit]...)
		for _, s := range prefix {
			if s.OrderId != nil && *s.OrderId == orderID {
				return dto.Plan{}, fmt.Errorf("Визит уже начался по модельному расписанию.")
			}
		}
		prefixes[r.StaffId] = prefix
		ids := []string{}
		for _, s := range r.Stops[limit:] {
			if s.Type == "VISIT" && s.OrderId != nil && *s.OrderId != orderID {
				ids = append(ids, *s.OrderId)
			}
		}
		sequences[r.StaffId] = ids
		if r.StaffId == source && source != target {
			next, ok := bestTail(ctx, w, w.Staff[source], ids, prefix)
			if !ok {
				return dto.Plan{}, fmt.Errorf("Не удалось сохранить остальные визиты прежнего исполнителя.")
			}
			p.Routes[i] = next
		}
	}
	if targetIndex < 0 {
		return dto.Plan{}, fmt.Errorf("У сотрудника нет активного маршрута этой смены.")
	}
	ids := sequences[target]
	var best dto.Plan
	found := false
	bestChanges := int(^uint(0) >> 1)
	var bestTravel int64
	for pos := 0; pos <= len(ids); pos++ {
		trial := append([]string{}, ids[:pos]...)
		trial = append(trial, orderID)
		trial = append(trial, ids[pos:]...)
		for b := 0; b <= len(trial); b++ {
			if ctx.Err() != nil {
				return dto.Plan{}, ctx.Err()
			}
			route, ok := tail(w, w.Staff[target], trial, b, prefixes[target])
			if !ok {
				continue
			}
			candidate := p
			candidate.Routes = append([]dto.Route{}, p.Routes...)
			candidate.Routes[targetIndex] = route
			candidate.Unassigned = []dto.UnassignedOrder{}
			for _, u := range p.Unassigned {
				if u.OrderId != orderID {
					candidate.Unassigned = append(candidate.Unassigned, u)
				}
			}
			changes := len(VisitChanges(old, candidate))
			var travel int64
			for i, s := range route.Stops {
				if i > 0 && s.Type != "BREAK" {
					sec, _, _ := w.Matrix.Travel(route.Stops[i-1].LocationId, s.LocationId)
					travel += sec
				}
			}
			if !found || changes < bestChanges || (changes == bestChanges && travel < bestTravel) {
				best = candidate
				found = true
				bestChanges = changes
				bestTravel = travel
			}
		}
	}
	if !found {
		return dto.Plan{}, fmt.Errorf("Не найдено место в маршруте с учётом окна, дороги, смены, обеда, недоступности и пополнения оборудования. Другие задания не снимаются.")
	}
	// Refresh mutates stop metadata; detach every slice from both source snapshots.
	for i := range best.Routes {
		best.Routes[i].Stops = append([]dto.Stop{}, best.Routes[i].Stops...)
	}
	Refresh(w, &best)
	if e := w.Validate(best); e != nil {
		return dto.Plan{}, fmt.Errorf("Назначение нарушает ограничения: %s", e)
	}
	return best, nil
}
