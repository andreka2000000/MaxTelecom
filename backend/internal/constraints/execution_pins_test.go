package constraints_test

import (
	"encoding/json"
	"errors"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"testing"
	"time"
)

func TestExecutionFactPinsFutureVisitAndOwner(t *testing.T) {
	for _, change := range []string{"time", "owner"} {
		t.Run(change, func(t *testing.T) {
			w, old := fixture(t)
			pinned := *visit(&old).OrderId
			if !c.Time(visit(&old).Start).After(c.Time(w.Day.Now)) {
				t.Fatal("fixture visit must be future")
			}
			var raw []byte
			var candidate dto.Plan
			if change == "owner" {
				source := old.Routes[0]
				person := w.Staff[source.StaffId]
				person.StaffId = "ST-PIN-TEST"
				w.Staff[person.StaffId] = person
				w.Reference.Staff = append(w.Reference.Staff, person)
				old.Routes = append(old.Routes, dto.Route{StaffId: person.StaffId, ServiceCenterId: person.ServiceCenterId, Status: "ACTIVE", Stops: []dto.Stop{}})
			}
			raw, _ = json.Marshal(old)
			if e := json.Unmarshal(raw, &candidate); e != nil {
				t.Fatal(e)
			}
			if change == "time" {
				for i := range candidate.Routes {
					for j := range candidate.Routes[i].Stops {
						stop := &candidate.Routes[i].Stops[j]
						stop.Arrival = c.Stamp(c.Time(stop.Arrival).Add(time.Minute))
						stop.Start = c.Stamp(c.Time(stop.Start).Add(time.Minute))
						stop.End = c.Stamp(c.Time(stop.End).Add(time.Minute))
					}
				}
			} else {
				last := len(candidate.Routes) - 1
				candidate.Routes[last].Stops = candidate.Routes[0].Stops
				candidate.Routes[0].Stops = []dto.Stop{}
			}
			w.Previous = &old
			if e := w.Validate(candidate); e != nil {
				t.Fatalf("future change without execution should be feasible: %v", e)
			}
			w.Pinned = map[string]bool{pinned: true}
			var violation *c.Violation
			if e := w.Validate(candidate); !errors.As(e, &violation) || violation.Code != "H10" {
				t.Fatalf("execution fact did not protect future %s: %v", change, e)
			}
		})
	}
}
