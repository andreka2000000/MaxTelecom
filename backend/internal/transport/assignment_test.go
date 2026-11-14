package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	c "magistral/backend/internal/constraints"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/planning"
	"magistral/backend/internal/storage/postgres"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

const assignmentDay = "/ui/v1/days/2026-10-26"
const assignmentOrder = "ORD-20261026-0001"

func assignmentSetup(t *testing.T) *accessCase {
	t.Helper()
	x := accessSetup(t)
	in, e := x.f.store.EventInputs(context.Background(), "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	person := in.Reference.Staff[0]
	person.StaffId = "ST-002"
	name := "Монтажник 002"
	person.DisplayName = &name
	in.Reference.Staff = append(in.Reference.Staff, person)
	s := &application.Service{Store: x.f.store}
	if e = s.Reference(context.Background(), in.Reference); e != nil {
		t.Fatal(e)
	}
	if e = s.Day(context.Background(), "2026-10-26", in.Day); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BuildPlan(context.Background(), "2026-10-26", 1); e != nil {
		t.Fatal(e)
	}
	u, e := x.manager.Create(context.Background(), auth.NewUser{Username: "technician-two", Name: "Second technician", Role: auth.Technician, StaffID: "ST-002", Password: testPassword})
	if e != nil {
		t.Fatal(e)
	}
	x.users[u.Username] = u
	x.cookies[u.Username] = x.call("", "POST", "/auth/v1/session", map[string]string{"username": u.Username, "password": testPassword}, 200).Result().Cookies()[0]
	return x
}
func previewAssignment(t *testing.T, x *accessCase) application.AssignmentPreview {
	t.Helper()
	var out application.AssignmentPreview
	if e := json.Unmarshal(x.call("dispatcher", "GET", assignmentDay+"/orders/"+assignmentOrder+"/assignment-options", nil, 200).Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func availableAssignment(t *testing.T, preview application.AssignmentPreview) application.AssignmentInput {
	t.Helper()
	for _, o := range preview.Options {
		if o.Available {
			if len(o.Changes) == 0 || o.Token == "" || o.Plan != nil {
				t.Fatal("incomplete preview or internal plan leaked")
			}
			return application.AssignmentInput{RequestID: "manual-test", StaffID: o.StaffID, Token: o.Token, Reason: "Согласовано с исполнителем"}
		}
	}
	t.Fatalf("no eligible choice: %+v", preview)
	return application.AssignmentInput{}
}
func assignmentSnapshot(t *testing.T, x *accessCase) postgres.Workspace {
	t.Helper()
	var out postgres.Workspace
	if e := json.Unmarshal(x.call("dispatcher", "GET", assignmentDay+"/workspace", nil, 200).Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func owner(p dto.Plan, id string) string {
	for _, r := range p.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == id {
				return r.StaffId
			}
		}
	}
	return ""
}

func TestAssignmentReassignAuditIdempotencyAndExecution(t *testing.T) {
	x := assignmentSetup(t)
	before := assignmentSnapshot(t, x)
	preview := previewAssignment(t, x)
	input := availableAssignment(t, preview)
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("preview mutated workspace")
	}
	path := assignmentDay + "/orders/" + assignmentOrder + "/assignment"
	for _, user := range []string{"technician", "subscriber"} {
		x.call(user, "GET", path+"-options", nil, 403)
		x.call(user, "POST", path, input, 403)
	}
	first := x.call("dispatcher", "POST", path, input, 200).Body.Bytes()
	if !bytes.Equal(first, x.call("dispatcher", "POST", path, input, 200).Body.Bytes()) {
		t.Fatal("retry response changed")
	}
	changed := input
	changed.Reason = "Different operation"
	x.call("dispatcher", "POST", path, changed, 409)
	after := assignmentSnapshot(t, x)
	if after.AssignmentCount != 1 || len(after.Assignments) != 1 || after.EventCount != 0 || after.Plan.PlanVersion != before.Plan.PlanVersion+1 || after.Day.Now != before.Day.Now {
		t.Fatal("manual change not atomic", after.Assignments)
	}
	r := after.Assignments[0]
	if r.ActorID != x.users["dispatcher"].ID || r.FromStaff != preview.CurrentStaff || r.ToStaff != input.StaffID || r.Reason != input.Reason {
		t.Fatal("audit incomplete", r)
	}
	if owner(*after.Plan, assignmentOrder) != input.StaffID || owner(*after.Plan, "ORD-20261026-0042") != owner(*before.Plan, "ORD-20261026-0042") {
		t.Fatal("assignment lost or unrelated owner changed")
	}
	for _, route := range before.Plan.Routes {
		if route.StaffId != preview.CurrentStaff && route.StaffId != input.StaffID {
			for _, actual := range after.Plan.Routes {
				if actual.StaffId == route.StaffId && !reflect.DeepEqual(route, actual) {
					t.Fatal("unaffected route changed")
				}
			}
		}
	}
	oldUser, newUser := "technician", "technician-two"
	if input.StaffID == "ST-001" {
		oldUser, newUser = newUser, oldUser
	}
	oldRaw := x.call(oldUser, "GET", assignmentDay+"/workspace", nil, 200).Body.Bytes()
	newRaw := x.call(newUser, "GET", assignmentDay+"/workspace", nil, 200).Body.Bytes()
	if bytes.Contains(oldRaw, []byte(assignmentOrder)) || !bytes.Contains(newRaw, []byte(assignmentOrder)) || bytes.Contains(newRaw, []byte(input.Reason)) || bytes.Contains(oldRaw, []byte(input.Reason)) {
		t.Fatal("personal routes not updated or audit leaked")
	}
	x.call("dispatcher", "POST", dayURL+"/plan", nil, 409)
	// Later event uses the manually saved plan and preserves the feasible choice.
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("after-manual", "08:31:00"), 200)
	current := assignmentSnapshot(t, x)
	if owner(*current.Plan, assignmentOrder) != input.StaffID || current.Plan.PlanVersion != after.Plan.PlanVersion+1 {
		t.Fatal("event lost manual assignment")
	}
	var visit dto.Stop
	for _, route := range current.Plan.Routes {
		for _, stop := range route.Stops {
			if stop.OrderId != nil && *stop.OrderId == assignmentOrder {
				visit = stop
			}
		}
	}
	x.call("dispatcher", "POST", assignmentDay+"/events", map[string]string{"event_id": "start-assigned", "type": "TIME_TICK", "timestamp": string(visit.Start)}, 200)
	execute := assignmentDay + "/orders/" + assignmentOrder + "/execution"
	x.call(oldUser, "POST", execute, map[string]string{"action": "START"}, 403)
	x.call(newUser, "POST", execute, map[string]string{"action": "START"}, 200)
	locked := previewAssignment(t, x)
	if locked.Allowed || locked.Reason == "" {
		t.Fatal("started visit is assignable")
	}
	input.RequestID = "cannot-reassign-started"
	x.call("dispatcher", "POST", path, input, 409)
	x.call(newUser, "POST", execute, map[string]string{"action": "COMPLETE", "report": "Связь восстановлена"}, 200)
	if previewAssignment(t, x).Allowed {
		t.Fatal("completed visit is assignable")
	}
}

func TestAssignmentStalePreviewConcurrencyAndSQLRollback(t *testing.T) {
	x := assignmentSetup(t)
	input := availableAssignment(t, previewAssignment(t, x))
	path := assignmentDay + "/orders/" + assignmentOrder + "/assignment"
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("invalidate-manual-preview", "08:31:00"), 200)
	before := assignmentSnapshot(t, x)
	response := x.call("dispatcher", "POST", path, input, 409)
	if !bytes.Contains(response.Body.Bytes(), []byte("ASSIGNMENT_STALE")) || !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("stale preview changed data")
	}
	input = availableAssignment(t, previewAssignment(t, x))
	_, e := x.f.store.Pool.Exec(context.Background(), `CREATE FUNCTION reject_assignment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected assignment failure'; END $$; CREATE TRIGGER reject_assignment BEFORE INSERT ON manual_assignments FOR EACH ROW EXECUTE FUNCTION reject_assignment()`)
	if e != nil {
		t.Fatal(e)
	}
	x.call("dispatcher", "POST", path, input, 500)
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("failed SQL partially published plan")
	}
	var snapshotCount int
	if e = x.f.store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM plan_snapshots").Scan(&snapshotCount); e != nil || snapshotCount != 2 {
		t.Fatal("orphan snapshot after rollback", snapshotCount, e)
	}
	if _, e = x.f.store.Pool.Exec(context.Background(), "DROP TRIGGER reject_assignment ON manual_assignments"); e != nil {
		t.Fatal(e)
	}
	// Simultaneous identical retries must publish one audit and one plan only.
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 2)
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", path, bytes.NewReader(encode(input)))
			req.AddCookie(x.cookies["dispatcher"])
			req.Header.Set("Origin", "http://127.0.0.1:3001")
			responses[i] = httptest.NewRecorder()
			x.f.api.ServeHTTP(responses[i], req)
		}(i)
	}
	wg.Wait()
	for _, rr := range responses {
		if rr.Code != http.StatusOK {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	if !bytes.Equal(responses[0].Body.Bytes(), responses[1].Body.Bytes()) {
		t.Fatal("concurrent retry differed")
	}
	after := assignmentSnapshot(t, x)
	if after.AssignmentCount != 1 || after.Plan.PlanVersion != before.Plan.PlanVersion+1 {
		t.Fatal("duplicate publication")
	}
}

func TestAssignmentUnassignedAndEligibilityReasons(t *testing.T) {
	x := assignmentSetup(t)
	ctx := context.Background()
	in, e := x.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	// Additional valid reference entries expose distinct reasons to the dispatcher.
	for i, id := range []string{"ST-003", "ST-004", "ST-005", "ST-006"} {
		person := in.Reference.Staff[0]
		person.StaffId = id
		person.HeightPermit = true
		switch i {
		case 0:
			person.Technologies = []dto.Technology{"FTTB"}
		case 1:
			person.HeightPermit = false
		case 2:
			person.EquipmentCapacity.ONT = 0
			person.EquipmentStart.ONT = 0
		case 3:
			in.Day.AbsentStaffIds = append(in.Day.AbsentStaffIds, id)
		}
		in.Reference.Staff = append(in.Reference.Staff, person)
	}
	for i := 0; i < 3; i++ {
		in.Reference.Staff[i].HeightPermit = true
	}
	in.Day.Orders[0].RequiresHeight = true
	s := &application.Service{Store: x.f.store}
	if e = s.Reference(ctx, in.Reference); e != nil {
		t.Fatal(e)
	}
	if e = s.Day(ctx, "2026-10-26", in.Day); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BuildPlan(ctx, "2026-10-26", 1); e != nil {
		t.Fatal(e)
	}
	inputs, e := x.f.store.PlanningInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	p, e := x.f.store.CurrentPlan(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	// A valid, feasible but unassigned job must be assignable too.
	for i := range p.Routes {
		if p.Routes[i].StaffId == owner(p, assignmentOrder) {
			p.Routes[i].Stops = []dto.Stop{}
		}
	}
	p.Unassigned = append(p.Unassigned, dto.UnassignedOrder{OrderId: assignmentOrder, ReasonCode: "NO_FEASIBLE_SLOT"})
	w, e := c.New(in.Reference, in.Day, in.Matrix)
	if e != nil {
		t.Fatal(e)
	}
	planning.Refresh(w, &p)
	if e = w.Validate(p); e != nil {
		t.Fatal(e)
	}
	if e = x.f.store.Publish(ctx, inputs, p); e != nil {
		t.Fatal(e)
	}
	preview := previewAssignment(t, x)
	if preview.CurrentStaff != "" {
		t.Fatal("not unassigned")
	}
	codes := map[string]string{}
	for _, o := range preview.Options {
		codes[o.StaffID] = o.Code
	}
	for id, want := range map[string]string{"ST-003": "TECHNOLOGY", "ST-004": "HEIGHT", "ST-005": "EQUIPMENT", "ST-006": "ABSENT", "ST-029": "PROFILE"} {
		if codes[id] != want {
			t.Fatal(id, "got", codes[id], "want", want)
		}
	}
	input := availableAssignment(t, preview)
	x.call("dispatcher", "POST", assignmentDay+"/orders/"+assignmentOrder+"/assignment", input, 200)
	final := assignmentSnapshot(t, x)
	if owner(*final.Plan, assignmentOrder) != input.StaffID || len(final.Plan.Unassigned) != 0 {
		t.Fatal("unassigned job not inserted")
	}
}

func TestAssignmentUpdatesSubscriberAndPreservesStartedVisit(t *testing.T) {
	x := assignmentSetup(t)
	id := newRequest(x)
	x.call("dispatcher", "POST", "/ui/v1/requests/"+id+"/approve", approvalInput(), 200)
	var requests []ServiceRequest
	json.Unmarshal(x.call("subscriber", "GET", "/ui/v1/requests", nil, 200).Body.Bytes(), &requests)
	orderID := requests[0].Order.ID
	before := assignmentSnapshot(t, x)
	var first dto.Stop
	firstStaff := owner(*before.Plan, assignmentOrder)
	for _, r := range before.Plan.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == assignmentOrder {
				first = stop
			}
		}
	}
	x.call("dispatcher", "POST", assignmentDay+"/events", map[string]string{"event_id": "freeze-first", "type": "TIME_TICK", "timestamp": string(first.Start)}, 200)
	firstUser := "technician"
	if firstStaff == "ST-002" {
		firstUser = "technician-two"
	}
	x.call(firstUser, "POST", assignmentDay+"/orders/"+assignmentOrder+"/execution", map[string]string{"action": "START"}, 200)
	before = assignmentSnapshot(t, x)
	for _, r := range before.Plan.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == assignmentOrder {
				first = stop
			}
		}
	}
	var preview application.AssignmentPreview
	json.Unmarshal(x.call("dispatcher", "GET", assignmentDay+"/orders/"+orderID+"/assignment-options", nil, 200).Body.Bytes(), &preview)
	input := availableAssignment(t, preview)
	x.call("dispatcher", "POST", assignmentDay+"/orders/"+orderID+"/assignment", input, 200)
	after := assignmentSnapshot(t, x)
	if len(after.Facts) != 1 || after.Facts[0].OrderID != assignmentOrder || after.Facts[0].Status != "IN_PROGRESS" {
		t.Fatal("started fact lost", after.Facts)
	}
	for _, r := range after.Plan.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == assignmentOrder && (r.StaffId != firstStaff || !reflect.DeepEqual(stop, first)) {
				t.Fatal("started visit moved")
			}
		}
	}
	requests = nil
	json.Unmarshal(x.call("subscriber", "GET", "/ui/v1/requests", nil, 200).Body.Bytes(), &requests)
	wantName := ""
	for _, s := range after.Reference.Staff {
		if s.StaffId == input.StaffID && s.DisplayName != nil {
			wantName = *s.DisplayName
		}
	}
	if requests[0].Order.StaffName != wantName || requests[0].Order.StaffID != "" || requests[0].Order.State != "SCHEDULED" {
		t.Fatal("client sees obsolete assignee", requests[0].Order)
	}
}

func TestAssignmentRejectsIneligibleAndReplacedDay(t *testing.T) {
	x := assignmentSetup(t)
	ctx := context.Background()
	input := availableAssignment(t, previewAssignment(t, x))
	path := assignmentDay + "/orders/" + assignmentOrder + "/assignment"
	before := assignmentSnapshot(t, x)
	bad := input
	bad.StaffID = "ST-029"
	x.call("dispatcher", "POST", path, bad, 422)
	bad = input
	bad.Reason = "  "
	x.call("dispatcher", "POST", path, bad, 422)
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("invalid choice changed state")
	}
	s := &application.Service{Store: x.f.store}
	if e := s.Day(ctx, "2026-10-26", before.Day); e != nil {
		t.Fatal(e)
	}
	x.call("dispatcher", "GET", path+"-options", nil, 409)
	if _, e := s.BuildPlan(ctx, "2026-10-26", 1); e != nil {
		t.Fatal(e)
	}
	response := x.call("dispatcher", "POST", path, input, 409)
	if !bytes.Contains(response.Body.Bytes(), []byte("ASSIGNMENT_STALE")) || assignmentSnapshot(t, x).AssignmentCount != 0 {
		t.Fatal("old generation preview reused")
	}
}
