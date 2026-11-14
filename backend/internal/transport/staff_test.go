package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"magistral/backend/internal/application"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func staffInput(t *testing.T, x *accessCase, id string) application.StaffEditInput {
	t.Helper()
	d := assignmentSnapshot(t, x)
	for _, s := range d.Reference.Staff {
		if s.StaffId == id {
			name := s.StaffId
			if s.DisplayName != nil {
				name = *s.DisplayName
			}
			absent := false
			for _, v := range d.Day.AbsentStaffIds {
				if v == id {
					absent = true
				}
			}
			return application.StaffEditInput{Generation: d.Generation, Revision: d.Revision, RequestID: "edit-" + id, Reason: "Учебное изменение данных", Settings: application.StaffSettings{Name: name, Profiles: s.Profiles, Technologies: s.Technologies, HeightPermit: s.HeightPermit, Shift: s.Shift, EquipmentStart: s.EquipmentStart, EquipmentCapacity: s.EquipmentCapacity, Absent: absent}}
		}
	}
	t.Fatal("staff missing", id)
	return application.StaffEditInput{}
}
func staffPreview(t *testing.T, x *accessCase, id string, input *application.StaffEditInput) application.StaffEditPreview {
	t.Helper()
	var preview application.StaffEditPreview
	json.Unmarshal(x.call("dispatcher", "POST", assignmentDay+"/staff/"+id+"/preview", input, 200).Body.Bytes(), &preview)
	if preview.Token == "" || preview.Plan != nil || preview.Inputs.Generation != 0 {
		t.Fatal("bad preview or internal inputs leaked")
	}
	input.Token = preview.Token
	return preview
}

func TestStaffCardPreviewReplansPersistsAndKeepsOtherDays(t *testing.T) {
	x := assignmentSetup(t)
	ctx := context.Background()
	x.call("dispatcher", "POST", assignmentDay+"/copy", map[string]string{"date": "2026-10-27"}, 201)
	otherBefore := x.call("dispatcher", "GET", "/ui/v1/days/2026-10-27/workspace", nil, 200).Body.Bytes()
	globalBefore, e := (&application.Service{Store: x.f.store}).ReadReference(ctx)
	if e != nil {
		t.Fatal(e)
	}
	before := assignmentSnapshot(t, x)
	input := staffInput(t, x, "ST-001")
	input.Settings.Name = "Обновлённый специалист"
	input.Settings.Profiles = []dto.Profile{"INSTALL"}
	input.Settings.Technologies = []dto.Technology{"FTTB", "PON"}
	input.Settings.HeightPermit = true
	input.Settings.Shift.Start = "11:00"
	input.Settings.Shift.End = "20:00"
	input.Settings.Shift.BreakWindow.From = "14:00"
	input.Settings.Shift.BreakWindow.To = "17:00"
	input.Settings.Shift.BreakDurationMin = 45
	input.Settings.EquipmentStart = dto.Equipment{ONT: 2, ROUTER: 2, STB: 1}
	input.Settings.EquipmentCapacity = dto.Equipment{ONT: 3, ROUTER: 4, STB: 2}
	preview := staffPreview(t, x, "ST-001", &input)
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("preview wrote data")
	}
	if len(preview.Changes) == 0 || preview.Changes[0].BeforeStaff != "ST-001" || preview.Changes[0].AfterStaff != "ST-002" {
		t.Fatal("impact omitted reassignment", preview.Changes)
	}
	path := assignmentDay + "/staff/ST-001"
	for _, u := range []string{"technician", "subscriber"} {
		x.call(u, "POST", path+"/preview", input, 403)
		x.call(u, "PATCH", path, input, 403)
	}
	first := x.call("dispatcher", "PATCH", path, input, 200).Body.Bytes()
	if !bytes.Equal(first, x.call("dispatcher", "PATCH", path, input, 200).Body.Bytes()) {
		t.Fatal("retry result differs")
	}
	conflict := input
	conflict.Reason = "Другой запрос"
	x.call("dispatcher", "PATCH", path, conflict, 409)
	after := assignmentSnapshot(t, x)
	if after.StaffChangeCount != 1 || len(after.StaffChanges) != 1 || after.EventCount != before.EventCount || after.Plan.PlanVersion != before.Plan.PlanVersion+1 || after.Day.Now != before.Day.Now || owner(*after.Plan, assignmentOrder) != "ST-002" {
		t.Fatal("card and plan not published together")
	}
	record := after.StaffChanges[0]
	if record.ActorID != x.users["dispatcher"].ID || record.Reason != input.Reason || record.After.EquipmentStart != input.Settings.EquipmentStart || record.After.Shift != input.Settings.Shift || *record.After.DisplayName != input.Settings.Name || !record.After.HeightPermit {
		t.Fatal("incomplete saved card/audit")
	}
	globalAfter, e := (&application.Service{Store: x.f.store}).ReadReference(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(globalBefore, globalAfter) || !bytes.Equal(otherBefore, x.call("dispatcher", "GET", "/ui/v1/days/2026-10-27/workspace", nil, 200).Body.Bytes()) {
		t.Fatal("other day or template modified")
	}
	personal := x.call("technician", "GET", assignmentDay+"/workspace", nil, 200).Body.Bytes()
	if bytes.Contains(personal, []byte(input.Reason)) || bytes.Contains(personal, []byte("ST-002")) || !bytes.Contains(personal, []byte(input.Settings.Name)) {
		t.Fatal("personal projection wrong")
	}
	x.call("dispatcher", "POST", dayURL+"/plan", nil, 409)
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("after-card", "08:31:00"), 200)
	current, e := x.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range current.Reference.Staff {
		if s.StaffId == "ST-001" && !reflect.DeepEqual(s, record.After) {
			t.Fatal("events did not read effective staff")
		}
	}
	x.call("dispatcher", "POST", assignmentDay+"/copy", map[string]string{"date": "2026-10-28"}, 201)
	var copied postgres.Workspace
	json.Unmarshal(x.call("dispatcher", "GET", "/ui/v1/days/2026-10-28/workspace", nil, 200).Body.Bytes(), &copied)
	if copied.StaffChangeCount != 0 || len(copied.StaffChanges) != 0 {
		t.Fatal("copy inherited old audit")
	}
	for _, s := range copied.Reference.Staff {
		if s.StaffId == "ST-001" && !reflect.DeepEqual(s, record.After) {
			t.Fatal("copy lost staff settings")
		}
	}
}

func TestStaffCardAbsenceBeforePlanRestoreAndUnassignedImpact(t *testing.T) {
	x := accessSetup(t)
	ctx := context.Background()
	before := assignmentSnapshot(t, x)
	s := &application.Service{Store: x.f.store}
	if e := s.Day(ctx, "2026-10-26", before.Day); e != nil {
		t.Fatal(e)
	}
	input := staffInput(t, x, "ST-001")
	input.Settings.Absent = true
	preview := staffPreview(t, x, "ST-001", &input)
	if preview.HasPlan {
		t.Fatal("preview built a plan unexpectedly")
	}
	x.call("dispatcher", "PATCH", assignmentDay+"/staff/ST-001", input, 200)
	prepared := assignmentSnapshot(t, x)
	if prepared.Plan != nil || len(prepared.Day.AbsentStaffIds) != 1 {
		t.Fatal("pre-plan settings not saved")
	}
	x.call("dispatcher", "POST", dayURL+"/plan", nil, 200)
	planned := assignmentSnapshot(t, x)
	if owner(*planned.Plan, assignmentOrder) != "" || len(planned.Plan.Unassigned) != 1 {
		t.Fatal("initial plan ignored absence")
	}
	input = staffInput(t, x, "ST-001")
	input.RequestID = "restore-employee"
	input.Settings.Absent = false
	preview = staffPreview(t, x, "ST-001", &input)
	if preview.VisitsAfter != preview.VisitsBefore+1 || len(preview.Changes) != 1 || preview.Changes[0].AfterStaff != "ST-001" {
		t.Fatal("return impact wrong", preview.Changes)
	}
	x.call("dispatcher", "PATCH", assignmentDay+"/staff/ST-001", input, 200)
	input = staffInput(t, x, "ST-001")
	input.RequestID = "remove-technology"
	input.Settings.Technologies = []dto.Technology{"FTTB"}
	preview = staffPreview(t, x, "ST-001", &input)
	if preview.UnassignedAfter != 1 || len(preview.Changes) != 1 || preview.Changes[0].AfterStaff != "" || preview.Changes[0].Reason == "" {
		t.Fatal("lost assignment missing from impact", preview.Changes)
	}
	x.call("dispatcher", "PATCH", assignmentDay+"/staff/ST-001", input, 200)
	options := previewAssignment(t, x)
	for _, o := range options.Options {
		if o.StaffID == "ST-001" && o.Code != "TECHNOLOGY" {
			t.Fatal("manual choice ignored staff edit")
		}
	}
}

func TestStaffCardStaleSQLRollbackAndConcurrentRetry(t *testing.T) {
	x := assignmentSetup(t)
	input := staffInput(t, x, "ST-001")
	input.Settings.Name = "Новое имя"
	staffPreview(t, x, "ST-001", &input)
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("invalidate-card", "08:31:00"), 200)
	x.call("dispatcher", "PATCH", assignmentDay+"/staff/ST-001", input, 409)
	input = staffInput(t, x, "ST-001")
	input.Settings.Name = "Новое имя"
	staffPreview(t, x, "ST-001", &input)
	before := assignmentSnapshot(t, x)
	_, e := x.f.store.Pool.Exec(context.Background(), `CREATE FUNCTION reject_staff_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected staff failure'; END $$; CREATE TRIGGER reject_staff_change BEFORE INSERT ON staff_changes FOR EACH ROW EXECUTE FUNCTION reject_staff_change()`)
	if e != nil {
		t.Fatal(e)
	}
	x.call("dispatcher", "PATCH", assignmentDay+"/staff/ST-001", input, 500)
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("SQL failure changed card or plan")
	}
	var snapshots int
	if e = x.f.store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM plan_snapshots").Scan(&snapshots); e != nil || snapshots != 2 {
		t.Fatal("orphan plan snapshot", snapshots, e)
	}
	if _, e = x.f.store.Pool.Exec(context.Background(), "DROP TRIGGER reject_staff_change ON staff_changes"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 2)
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("PATCH", assignmentDay+"/staff/ST-001", bytes.NewReader(encode(input)))
			req.AddCookie(x.cookies["dispatcher"])
			responses[i] = httptest.NewRecorder()
			x.f.api.ServeHTTP(responses[i], req)
		}(i)
	}
	wg.Wait()
	for _, rr := range responses {
		if rr.Code != 200 {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	if !bytes.Equal(responses[0].Body.Bytes(), responses[1].Body.Bytes()) || assignmentSnapshot(t, x).StaffChangeCount != 1 {
		t.Fatal("duplicate staff change")
	}
}

func TestStaffCardValidationAndFrozenHistory(t *testing.T) {
	x := assignmentSetup(t)
	before := assignmentSnapshot(t, x)
	for _, mutate := range []func(*application.StaffSettings){func(s *application.StaffSettings) { s.EquipmentStart.ONT = -1 }, func(s *application.StaffSettings) { s.EquipmentStart.ONT = s.EquipmentCapacity.ONT + 1 }, func(s *application.StaffSettings) { s.Shift.Start = "20:00" }, func(s *application.StaffSettings) { s.Technologies = []dto.Technology{"invalid"} }, func(s *application.StaffSettings) { s.Profiles = nil }, func(s *application.StaffSettings) { s.Shift.BreakDurationMin = 1000 }} {
		input := staffInput(t, x, "ST-001")
		mutate(&input.Settings)
		x.call("dispatcher", "POST", assignmentDay+"/staff/ST-001/preview", input, 422)
	}
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) {
		t.Fatal("invalid preview changed data")
	}
	var departure dto.DateTime
	for _, r := range before.Plan.Routes {
		if r.StaffId == "ST-001" {
			departure = r.Stops[0].Start
		}
	}
	x.call("dispatcher", "POST", assignmentDay+"/events", map[string]string{"event_id": "freeze-card", "type": "TIME_TICK", "timestamp": string(departure)}, 200)
	input := staffInput(t, x, "ST-001")
	input.Settings.Name = "Нельзя переписать историю"
	response := x.call("dispatcher", "POST", assignmentDay+"/staff/ST-001/preview", input, 409)
	if !bytes.Contains(response.Body.Bytes(), []byte("STAFF_HISTORY_LOCKED")) {
		t.Fatal("history not protected")
	}
}
