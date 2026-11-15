package transport

import (
	"context"
	"encoding/json"
	"errors"
	"magistral/backend/internal/application"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"reflect"
	"testing"
	"time"
)

func journeyEmptyCase(t *testing.T) (*accessCase, string, dto.Stop) {
	t.Helper()
	x := assignmentSetup(t)
	ctx := context.Background()
	in, e := x.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	in.Day.Orders = []dto.Order{}
	service := &application.Service{Store: x.f.store}
	if e = service.Day(ctx, "2026-10-26", in.Day); e != nil {
		t.Fatal(e)
	}
	if _, e = service.BuildPlan(ctx, "2026-10-26", 1); e != nil {
		t.Fatal(e)
	}
	id := newRequest(x)
	x.call("dispatcher", "POST", "/ui/v1/requests/"+id+"/approve", approvalInput(), 200)
	requests := journeyRequests(t, x)
	if len(requests) != 1 || requests[0].Order == nil {
		t.Fatal("request not scheduled")
	}
	orderID := requests[0].Order.ID
	plan, e := x.f.store.CurrentPlan(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	for _, route := range plan.Routes {
		for _, stop := range route.Stops {
			if stop.Type == "VISIT" && stop.OrderId != nil && *stop.OrderId == orderID {
				if route.StaffId != "ST-001" {
					t.Fatal("expected primary technician", route.StaffId)
				}
				return x, orderID, stop
			}
		}
	}
	t.Fatal("approved request has no visit")
	return nil, "", dto.Stop{}
}
func journeyRequests(t *testing.T, x *accessCase) []ServiceRequest {
	t.Helper()
	var out []ServiceRequest
	if e := json.Unmarshal(x.call("subscriber", "GET", "/ui/v1/requests", nil, 200).Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func journeyWorkspace(t *testing.T, x *accessCase, user string) postgres.Workspace {
	t.Helper()
	var out postgres.Workspace
	if e := json.Unmarshal(x.call(user, "GET", assignmentDay+"/workspace", nil, 200).Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func journeyFact(t *testing.T, x *accessCase, id string) postgres.VisitFact {
	t.Helper()
	for _, f := range journeyWorkspace(t, x, "technician").Facts {
		if f.OrderID == id {
			return f
		}
	}
	t.Fatal("execution fact missing")
	return postgres.VisitFact{}
}
func journeyTick(t *testing.T, x *accessCase, id string, at time.Time) {
	t.Helper()
	x.call("dispatcher", "POST", assignmentDay+"/events", dto.EventInput{EventId: id, Type: "TIME_TICK", Timestamp: dto.DateTime(at.Format(time.RFC3339))}, 200)
}
func journeyDeparture(stop dto.Stop) time.Time {
	at, _ := time.Parse(time.RFC3339, string(stop.Arrival))
	if stop.TravelSec != nil {
		at = at.Add(-time.Duration(*stop.TravelSec) * time.Second)
	}
	return at
}
func journeyAssertState(t *testing.T, x *accessCase, state string) *RequestOrder {
	t.Helper()
	requests := journeyRequests(t, x)
	if len(requests) != 1 || requests[0].Order == nil || requests[0].Order.State != state {
		t.Fatalf("subscriber state did not become %s: %+v", state, requests)
	}
	return requests[0].Order
}
func journeyAssertCode(t *testing.T, raw []byte, code string) {
	t.Helper()
	var failure dto.ErrorResponse
	if e := json.Unmarshal(raw, &failure); e != nil {
		t.Fatal(e)
	}
	if failure.Error.Code != code {
		t.Fatalf("got error %s, want %s", failure.Error.Code, code)
	}
}

func TestJourneyFullCyclePersistsTimesAndProtectsFutureVisit(t *testing.T) {
	x, id, visit := journeyEmptyCase(t)
	execute := assignmentDay + "/orders/" + id + "/execution"
	departure := journeyDeparture(visit)
	start, _ := time.Parse(time.RFC3339, string(visit.Start))
	if !start.After(departure.Add(2 * time.Minute)) {
		t.Fatal("fixture needs time before visit")
	}
	journeyAssertCode(t, x.call("technician", "POST", execute, map[string]string{"action": "DEPART"}, 409).Body.Bytes(), "DEPARTURE_NOT_DUE")
	journeyAssertCode(t, x.call("technician", "POST", execute, map[string]string{"action": "ARRIVE"}, 409).Body.Bytes(), "DEPARTURE_REQUIRED")
	for _, user := range []string{"technician-two", "subscriber"} {
		x.call(user, "POST", execute, map[string]string{"action": "DEPART"}, 403)
	}
	journeyTick(t, x, "journey-departure-clock", departure)
	before := journeyWorkspace(t, x, "dispatcher")
	x.call("technician", "POST", execute, map[string]string{"action": "DEPART"}, 200)
	first := journeyFact(t, x, id)
	if first.Status != "EN_ROUTE" || first.DepartedAt != departure.Format(time.RFC3339) || first.ArrivedAt != "" || first.StartedAt != "" {
		t.Fatal("departure confused with work start", first)
	}
	if !reflect.DeepEqual(before.Plan, journeyWorkspace(t, x, "dispatcher").Plan) {
		t.Fatal("departure changed organizer plan")
	}
	request := journeyAssertState(t, x, "EN_ROUTE")
	if request.DepartedAt != first.DepartedAt || request.StartedAt != "" {
		t.Fatal("subscriber departure times incorrect", request)
	}
	journeyAssertCode(t, x.call("technician", "POST", execute, map[string]string{"action": "START"}, 409).Body.Bytes(), "ARRIVAL_REQUIRED")
	journeyTick(t, x, "journey-in-transit-clock", departure.Add(time.Minute))
	x.call("technician", "POST", execute, map[string]string{"action": "DEPART"}, 200)
	if !reflect.DeepEqual(first, journeyFact(t, x, id)) {
		t.Fatal("departure retry rewrote fact")
	}
	after := journeyWorkspace(t, x, "dispatcher")
	if len(after.Plan.Routes) != len(before.Plan.Routes) {
		t.Fatal("replan removed route")
	}
	for i, route := range before.Plan.Routes {
		for j, stop := range route.Stops {
			if stop.OrderId != nil && *stop.OrderId == id {
				next := after.Plan.Routes[i].Stops[j]
				if route.StaffId != after.Plan.Routes[i].StaffId || stop.Start != next.Start || stop.End != next.End || stop.Arrival != next.Arrival || *next.OrderId != id {
					t.Fatal("clock moved active future visit")
				}
			}
		}
	}
	for _, kind := range []string{"ORDER_CANCELLED", "ORDER_TRANSFERRED", "VISIT_DELAYED"} {
		event := dto.EventInput{EventId: "journey-lock-" + kind, Type: dto.EventType(kind), Timestamp: dto.DateTime(departure.Add(time.Minute).Format(time.RFC3339)), OrderId: id, StaffId: "ST-001"}
		switch kind {
		case "ORDER_CANCELLED":
			event.Reason = "CUSTOMER_REQUEST"
		case "ORDER_TRANSFERRED":
			event.ActualEnd = &event.Timestamp
			event.TransferredTo = "STATION_EQUIPMENT"
		case "VISIT_DELAYED":
			end := dto.DateTime(departure.Add(4 * time.Hour).Format(time.RFC3339))
			event.ExpectedEnd = &end
		}
		journeyAssertCode(t, x.call("dispatcher", "POST", assignmentDay+"/events", event, 409).Body.Bytes(), "VISIT_EXECUTION_LOCKED")
	}
	journeyTick(t, x, "journey-arrival-clock", departure.Add(2*time.Minute))
	x.call("technician", "POST", execute, map[string]string{"action": "ARRIVE"}, 200)
	arrived := journeyFact(t, x, id)
	if arrived.Status != "ARRIVED" || arrived.StartedAt != "" || arrived.ArrivedAt != departure.Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatal("arrival confused with work start", arrived)
	}
	journeyAssertState(t, x, "ARRIVED")
	x.call("technician", "POST", execute, map[string]string{"action": "START"}, 409)
	journeyTick(t, x, "journey-work-clock", start)
	x.call("technician", "POST", execute, map[string]string{"action": "START"}, 200)
	journeyAssertState(t, x, "IN_PROGRESS")
	journeyTick(t, x, "journey-finish-clock", start.Add(5*time.Minute))
	beforeComplete := journeyWorkspace(t, x, "dispatcher")
	x.call("technician", "POST", execute, map[string]string{"action": "COMPLETE", "report": "Восстановлена связь, проверено оборудование"}, 200)
	completed := journeyFact(t, x, id)
	if completed.Status != "COMPLETED" || completed.StartedAt != start.Format(time.RFC3339) || completed.CompletedAt != start.Add(5*time.Minute).Format(time.RFC3339) {
		t.Fatal("completion times incorrect", completed)
	}
	request = journeyAssertState(t, x, "COMPLETED")
	if request.DepartedAt != completed.DepartedAt || request.ArrivedAt != completed.ArrivedAt || request.Report != completed.Report {
		t.Fatal("subscriber execution history incomplete", request)
	}
	for _, action := range []string{"DEPART", "ARRIVE", "START", "COMPLETE"} {
		x.call("technician", "POST", execute, map[string]string{"action": action, "report": "Повторная отправка отчёта"}, 200)
	}
	if !reflect.DeepEqual(completed, journeyFact(t, x, id)) {
		t.Fatal("retry changed completed history")
	}
	if !reflect.DeepEqual(beforeComplete.Plan, journeyWorkspace(t, x, "dispatcher").Plan) {
		t.Fatal("execution completion changed organizer plan")
	}
}

func TestJourneyBlocksAnotherActiveJobUntilCompletion(t *testing.T) {
	x := accessSetup(t)
	requestID := newRequest(x)
	x.call("dispatcher", "POST", "/ui/v1/requests/"+requestID+"/approve", approvalInput(), 200)
	plan, e := x.f.store.CurrentPlan(context.Background(), "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	visits := []dto.Stop{}
	var last time.Time
	for _, route := range plan.Routes {
		if route.StaffId != "ST-001" {
			continue
		}
		for _, stop := range route.Stops {
			if stop.Type != "VISIT" {
				continue
			}
			visits = append(visits, stop)
			at, _ := time.Parse(time.RFC3339, string(stop.Start))
			if at.After(last) {
				last = at
			}
		}
	}
	if len(visits) < 2 {
		t.Fatal("two visits required")
	}
	journeyTick(t, x, "journey-busy-clock", last.Add(time.Minute))
	first := assignmentDay + "/orders/" + *visits[0].OrderId + "/execution"
	second := assignmentDay + "/orders/" + *visits[1].OrderId + "/execution"
	journeyAssertCode(t, x.call("technician", "POST", second, map[string]string{"action": "DEPART"}, 409).Body.Bytes(), "PREVIOUS_VISIT_UNFINISHED")
	x.call("technician", "POST", first, map[string]string{"action": "DEPART"}, 200)
	journeyAssertCode(t, x.call("technician", "POST", second, map[string]string{"action": "START"}, 409).Body.Bytes(), "STAFF_BUSY")
	x.call("technician", "POST", first, map[string]string{"action": "ARRIVE"}, 200)
	journeyAssertCode(t, x.call("technician", "POST", second, map[string]string{"action": "START"}, 409).Body.Bytes(), "STAFF_BUSY")
	x.call("technician", "POST", first, map[string]string{"action": "START"}, 200)
	x.call("technician", "POST", first, map[string]string{"action": "COMPLETE", "report": "Работа выполнена"}, 200)
	x.call("technician", "POST", second, map[string]string{"action": "DEPART"}, 200)
}

func TestJourneyContactFollowsCurrentOwnerAndNeverStaleGeneration(t *testing.T) {
	x, id, _ := journeyEmptyCase(t)
	own := journeyWorkspace(t, x, "technician")
	foreign := journeyWorkspace(t, x, "technician-two")
	if len(own.Contacts) != 1 || own.Contacts[0].OrderID != id || own.Contacts[0].Phone != "+79990000000" || own.Contacts[0].Name != "Subscriber one" || own.Contacts[0].Description != "Не работает интернет" || len(foreign.Contacts) != 0 {
		t.Fatal("contact leaked or missing", own.Contacts, foreign.Contacts)
	}
	x.call("technician", "GET", "/ui/v1/requests", nil, 403)
	var preview application.AssignmentPreview
	if e := json.Unmarshal(x.call("dispatcher", "GET", assignmentDay+"/orders/"+id+"/assignment-options", nil, 200).Body.Bytes(), &preview); e != nil {
		t.Fatal(e)
	}
	input := availableAssignment(t, preview)
	if input.StaffID != "ST-002" {
		t.Fatal("expected second qualified technician")
	}
	x.call("dispatcher", "POST", assignmentDay+"/orders/"+id+"/assignment", input, 200)
	if len(journeyWorkspace(t, x, "technician").Contacts) != 0 || len(journeyWorkspace(t, x, "technician-two").Contacts) != 1 {
		t.Fatal("contact access did not follow reassignment")
	}
	ctx := context.Background()
	current, e := x.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	service := &application.Service{Store: x.f.store}
	if e = service.Day(ctx, "2026-10-26", current.Day); e != nil {
		t.Fatal(e)
	}
	if _, e = service.BuildPlan(ctx, "2026-10-26", 1); e != nil {
		t.Fatal(e)
	}
	if len(journeyWorkspace(t, x, "dispatcher").Contacts) != 0 || len(journeyWorkspace(t, x, "technician").Contacts) != 0 || len(journeyWorkspace(t, x, "technician-two").Contacts) != 0 {
		t.Fatal("old generation revived private contact")
	}
	journeyAssertState(t, x, "REQUIRES_REVIEW")
}

func TestJourneyFactBlocksFullPlanRebuildAndStalePublish(t *testing.T) {
	x := accessSetup(t)
	ctx := context.Background()
	service := &application.Service{Store: x.f.store}
	original, e := x.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	original.Day.Now = "2026-10-26T09:00:00+05:00"
	if e = service.Day(ctx, "2026-10-26", original.Day); e != nil {
		t.Fatal(e)
	}
	plan, e := service.BuildPlan(ctx, "2026-10-26", 1)
	if e != nil {
		t.Fatal(e)
	}
	inputs, e := x.f.store.PlanningInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	if inputs.EventCount != 0 {
		t.Fatal("fixture must isolate execution fact from accepted events")
	}
	x.call("technician", "POST", assignmentDay+"/orders/"+assignmentOrder+"/execution", map[string]string{"action": "DEPART"}, 200)
	if _, e = x.f.store.PlanningInputs(ctx, "2026-10-26"); !errors.Is(e, postgres.ErrEvents) {
		t.Fatal("planning inputs ignored execution fact", e)
	}
	if e = x.f.store.Publish(ctx, inputs, plan); !errors.Is(e, postgres.ErrEvents) {
		t.Fatal("in-flight full rebuild overwrote execution history", e)
	}
	x.call("dispatcher", "POST", dayURL+"/plan", map[string]int{"time_limit_sec": 1}, 409)
	current, e := x.f.store.CurrentPlan(ctx, "2026-10-26")
	if e != nil || !reflect.DeepEqual(plan, current) {
		t.Fatal("rebuild guard changed existing plan", e)
	}
}
