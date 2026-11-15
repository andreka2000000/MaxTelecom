package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"magistral/backend/internal/dto"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestEventIDRejectsChangedPayloadAndRetainsLegacyRetries(t *testing.T) {
	f := eventFixture(t)
	f.build()
	first := tick("payload-id", "08:45:00")
	f.event(first, 200)
	before, e := f.store.Workspace(context.Background(), "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	changed := tick("payload-id", "08:46:00")
	f.event(changed, 409)
	after, _ := f.store.Workspace(context.Background(), "2026-10-26")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("conflict mutated workspace")
	}
	f.restart()
	if !f.event(first, 200).Duplicate {
		t.Fatal("retry after restart was not deduplicated")
	}
	if _, e = f.store.Pool.Exec(context.Background(), "UPDATE events SET request_hash='' WHERE event_id='payload-id'"); e != nil {
		t.Fatal(e)
	}
	if !f.event(first, 200).Duplicate {
		t.Fatal("legacy journal retry failed")
	}
	f.event(changed, 409)
	newOrder := f.newOrder("ORD-20261026-0099", "08:45:00")
	f.event(newOrder, 200)
	newOrder.Order.Address = "Другой адрес под прежним идентификатором"
	f.event(newOrder, 409)
}

func TestConcurrentDifferentPayloadsCommitExactlyOneEvent(t *testing.T) {
	f := eventFixture(t)
	f.build()
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, at := range []string{"08:40:00", "08:41:00"} {
		wg.Add(1)
		go func(at string) {
			defer wg.Done()
			r := httptest.NewRecorder()
			f.api.ServeHTTP(r, httptest.NewRequest("POST", dayURL+"/events", bytes.NewReader(encode(tick("collision", at)))))
			results <- r
		}(at)
	}
	wg.Wait()
	close(results)
	accepted, conflict := 0, 0
	for r := range results {
		switch r.Code {
		case 200:
			accepted++
		case 409:
			var failure dto.ErrorResponse
			json.Unmarshal(r.Body.Bytes(), &failure)
			if failure.Error.Code != "EVENT_ID_CONFLICT" {
				t.Fatal(r.Body.String())
			}
			conflict++
		default:
			t.Fatal(r.Code, r.Body.String())
		}
	}
	in, e := f.store.EventInputs(context.Background(), "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	if accepted != 1 || conflict != 1 || in.EventCount != 1 || in.Plan.PlanVersion != 2 {
		t.Fatal("conflicting writers committed more than one change", accepted, conflict, in.EventCount)
	}
}

func TestLiveEventRetryUsesOriginalPayloadBeforeServerTimestamp(t *testing.T) {
	x, _, wall := clockSetup(t, "2026-10-26T08:45:17+05:00")
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", modeInput(t, x, "LIVE", "live-reliability"), 200)
	until := dto.DateTime("2026-10-26T08:46:00+05:00")
	ev := dto.EventInput{EventId: "live-request", Type: "STAFF_UNAVAILABLE", Timestamp: "2026-10-26T08:45:00+05:00", StaffId: "ST-029", Reason: "ILLNESS", Until: &until}
	x.call("dispatcher", "POST", assignmentDay+"/events", ev, 200)
	before := assignmentSnapshot(t, x)
	*wall = wall.Add(time.Minute)
	var retry dto.EventResult
	json.Unmarshal(x.call("dispatcher", "POST", assignmentDay+"/events", ev, 200).Body.Bytes(), &retry)
	if !retry.Duplicate || !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("LIVE retry applied again or moved clock")
	}
	until = "2026-10-26T08:47:00+05:00"
	x.call("dispatcher", "POST", assignmentDay+"/events", ev, 409)
}

func TestActiveJourneyInfeasibleReplanLeavesEveryStoredFactUntouched(t *testing.T) {
	x, id, visit := journeyEmptyCase(t)
	departure, _ := time.Parse(time.RFC3339, string(visit.Arrival))
	if visit.TravelSec != nil {
		departure = departure.Add(-time.Duration(*visit.TravelSec) * time.Second)
	}
	journeyTick(t, x, "reliability-departure-clock", departure)
	x.call("technician", "POST", assignmentDay+"/orders/"+id+"/execution", map[string]string{"action": "DEPART"}, 200)
	before := journeyWorkspace(t, x, "dispatcher")
	ev := dto.EventInput{EventId: "active-outage", Type: "STAFF_UNAVAILABLE", Timestamp: dto.DateTime(departure.Format(time.RFC3339)), StaffId: "ST-001", Reason: "ILLNESS"}
	x.call("dispatcher", "POST", assignmentDay+"/events", ev, 422)
	if !reflect.DeepEqual(before, journeyWorkspace(t, x, "dispatcher")) {
		t.Fatal("failed repair changed clock, facts, contacts, journal or plan")
	}
}

func TestWorkingVisitDelayKeepsOwnerAndExecutionFacts(t *testing.T) {
	x, id, visit := journeyEmptyCase(t)
	start, e := time.Parse(time.RFC3339, string(visit.Start))
	if e != nil {
		t.Fatal(e)
	}
	end, e := time.Parse(time.RFC3339, string(visit.End))
	if e != nil {
		t.Fatal(e)
	}
	journeyTick(t, x, "delay-work-clock", start)
	execute := assignmentDay + "/orders/" + id + "/execution"
	for _, action := range []string{"DEPART", "ARRIVE", "START"} {
		x.call("technician", "POST", execute, map[string]string{"action": action}, 200)
	}
	before := journeyWorkspace(t, x, "dispatcher")
	expected := dto.DateTime(end.Add(15 * time.Minute).Format(time.RFC3339))
	ev := dto.EventInput{EventId: "confirmed-work-delay", Type: "VISIT_DELAYED", Timestamp: visit.Start, OrderId: id, StaffId: "ST-001", ExpectedEnd: &expected}
	x.call("technician", "POST", assignmentDay+"/events", ev, 200)
	after := journeyWorkspace(t, x, "dispatcher")
	if !reflect.DeepEqual(before.Facts, after.Facts) || !reflect.DeepEqual(before.Contacts, after.Contacts) || len(after.Day.Orders) != len(before.Day.Orders) {
		t.Fatal("delay changed execution or request data")
	}
	found := false
	for _, route := range after.Plan.Routes {
		for _, stop := range route.Stops {
			if stop.OrderId != nil && *stop.OrderId == id {
				found = true
				if route.StaffId != "ST-001" || stop.Start != visit.Start || stop.Arrival != visit.Arrival || stop.End != expected {
					t.Fatal("active visit moved or delay ignored")
				}
			}
		}
	}
	if !found || after.EventCount != before.EventCount+1 {
		t.Fatal("delay not committed exactly once")
	}
}
