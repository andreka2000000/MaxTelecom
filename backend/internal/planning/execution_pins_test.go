package planning

import (
	"context"

	"magistral/backend/internal/domain"
	"magistral/backend/internal/dto"
	"testing"
)

func firstExecutionVisit(t *testing.T, p dto.Plan) (string, string) {
	t.Helper()
	for _, route := range p.Routes {
		for _, stop := range route.Stops {
			if stop.Type == "VISIT" && stop.OrderId != nil {
				return *stop.OrderId, route.StaffId
			}
		}
	}
	t.Fatal("fixture has no visit")
	return "", ""
}

func TestRepairRejectsUnavailabilityThatWouldMoveActiveJourney(t *testing.T) {
	w := load(t)
	old, e := Build(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	id, staff := firstExecutionVisit(t, old)
	w.Previous = &old
	until := dto.DateTime("2026-10-26T10:00:00+05:00")
	w.Unavailable = map[string][]domain.Unavailability{staff: {{From: w.Day.Now, Until: &until}}}
	if _, e = Repair(context.Background(), w, old, map[string]dto.CancelledOrder{}, nil); e != nil {
		t.Fatalf("future visit should be reschedulable without departure: %v", e)
	}
	w.Pinned = map[string]bool{id: true}
	if _, e = Repair(context.Background(), w, old, map[string]dto.CancelledOrder{}, nil); e == nil {
		t.Fatal("active journey was silently rescheduled")
	}
}

func TestManualAssignmentCannotMoveFutureActiveJourney(t *testing.T) {
	w := load(t)
	old, e := Build(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	id, source := firstExecutionVisit(t, old)
	person := w.Staff[source]
	person.StaffId = "ST-PIN-TEST"
	w.Staff[person.StaffId] = person
	w.Reference.Staff = append(w.Reference.Staff, person)
	old.Routes = append(old.Routes, dto.Route{StaffId: person.StaffId, ServiceCenterId: person.ServiceCenterId, Status: "ACTIVE", Stops: []dto.Stop{}})
	w.Previous = &old
	if _, e = Assign(context.Background(), w, old, id, person.StaffId); e != nil {
		t.Fatalf("future reassignment without departure should be feasible: %v", e)
	}
	w.Pinned = map[string]bool{id: true}
	if _, e = Assign(context.Background(), w, old, id, person.StaffId); e == nil {
		t.Fatal("manual assignment moved a future active journey")
	}
}
