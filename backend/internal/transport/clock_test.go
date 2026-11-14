package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"magistral/backend/internal/application"
	"magistral/backend/internal/storage/postgres"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func clockSetup(t *testing.T, at string) (*accessCase, *application.Service, *time.Time) {
	x := accessSetup(t)
	wall, e := time.Parse(time.RFC3339, at)
	if e != nil {
		t.Fatal(e)
	}
	service := &application.Service{Store: x.f.store, Now: func() time.Time { return wall }}
	x.f.api = (&API{Service: service, Validator: x.f.validator}).ApplicationHandler(x.manager, []string{"http://127.0.0.1:3001"}, false)
	return x, service, &wall
}
func modeInput(t *testing.T, x *accessCase, mode, id string) application.ClockModeInput {
	t.Helper()
	var c postgres.ShiftClock
	json.Unmarshal(x.call("dispatcher", "GET", assignmentDay+"/clock", nil, 200).Body.Bytes(), &c)
	return application.ClockModeInput{Mode: mode, RequestID: id, Generation: c.Generation, Revision: c.Revision}
}
func TestClockModesServerTicksExecutionAndFreeze(t *testing.T) {
	x, s, wall := clockSetup(t, "2026-10-26T08:45:17+05:00")
	ctx := context.Background()
	before := assignmentSnapshot(t, x)
	var c postgres.ShiftClock
	json.Unmarshal(x.call("technician", "GET", assignmentDay+"/clock", nil, 200).Body.Bytes(), &c)
	if c.Mode != "DEMO" || c.State != "PAUSED" || c.ServerNow != "2026-10-26T08:45:17+05:00" {
		t.Fatal("wrong defaults", c)
	}
	input := modeInput(t, x, "LIVE", "enable-clock")
	for _, user := range []string{"technician", "subscriber"} {
		x.call(user, "PATCH", assignmentDay+"/clock", input, 403)
		x.call(user, "POST", assignmentDay+"/clock/sync", nil, 403)
		x.call(user, "GET", assignmentDay+"/clock/history", nil, 403)
	}
	first := x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 200).Body.Bytes()
	if !bytes.Equal(first, x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 200).Body.Bytes()) {
		t.Fatal("mode retry not idempotent")
	}
	enabled := assignmentSnapshot(t, x)
	if string(enabled.Day.Now) != "2026-10-26T08:45:17+05:00" || enabled.Plan.PlanVersion != before.Plan.PlanVersion+1 {
		t.Fatal("clock and plan not atomic")
	}
	*wall = wall.Add(2 * time.Minute)
	if e := s.SyncClock(ctx, "2026-10-26", false); e != nil {
		t.Fatal(e)
	}
	ticked := assignmentSnapshot(t, x)
	if string(ticked.Day.Now) != "2026-10-26T08:47:00+05:00" {
		t.Fatal("automatic tick not minute based", ticked.Day.Now)
	}
	if e := s.SyncClock(ctx, "2026-10-26", false); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(ticked, assignmentSnapshot(t, x)) {
		t.Fatal("unchanged minute created event")
	}
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("forbidden-live-manual", "09:00:00"), 409)
	var start string
	for _, r := range ticked.Plan.Routes {
		for _, stop := range r.Stops {
			if stop.OrderId != nil && *stop.OrderId == assignmentOrder {
				start = string(stop.Start)
			}
		}
	}
	*wall, _ = time.Parse(time.RFC3339, start)
	*wall = wall.Add(7 * time.Second)
	if e := s.SyncClock(ctx, "2026-10-26", true); e != nil {
		t.Fatal(e)
	}
	if len(assignmentSnapshot(t, x).Facts) != 0 {
		t.Fatal("clock invented execution facts")
	}
	x.call("technician", "POST", assignmentDay+"/orders/"+assignmentOrder+"/execution", map[string]string{"action": "START"}, 200)
	running := assignmentSnapshot(t, x)
	if len(running.Facts) != 1 || running.Facts[0].StartedAt != wall.Format(time.RFC3339) {
		t.Fatal("execution did not use server seconds", running.Facts)
	}
	freeze := modeInput(t, x, "DEMO", "freeze-clock")
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", freeze, 200)
	paused := assignmentSnapshot(t, x)
	*wall = wall.Add(time.Hour)
	if e := s.SyncClock(ctx, "2026-10-26", true); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(paused, assignmentSnapshot(t, x)) {
		t.Fatal("demo clock advanced automatically")
	}
	x.call("dispatcher", "POST", assignmentDay+"/events", tick("no-rewind", "08:30:00"), 409)
	history, e := x.f.store.ClockChanges(ctx, "2026-10-26")
	if e != nil || len(history) != 2 || history[0].ActorID != x.users["dispatcher"].ID {
		t.Fatal("clock audit missing", history, e)
	}
}
func TestClockCreationNoPlanRestartMidnightAndCopy(t *testing.T) {
	x, s, wall := clockSetup(t, "2026-10-26T23:58:09+05:00")
	ctx := context.Background()
	x.call("dispatcher", "POST", "/ui/v1/days", map[string]string{"date": "2026-10-27", "clock_mode": "LIVE"}, 409)
	// New live day before first plan, without a browser performing sync requests.
	before := assignmentSnapshot(t, x)
	if e := s.Day(ctx, "2026-10-26", before.Day); e != nil {
		t.Fatal(e)
	}
	input := modeInput(t, x, "LIVE", "no-plan-live")
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 200)
	*wall = wall.Add(time.Minute)
	recovered, e := postgres.Open(ctx, x.f.url)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Pool.Close()
	service := &application.Service{Store: recovered, Now: func() time.Time { return *wall }}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); service.RunClocks(run) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, e := recovered.Clock(ctx, "2026-10-26")
		if e != nil {
			t.Fatal(e)
		}
		if c.ModelNow == "2026-10-26T23:59:00+05:00" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("scheduler did not recover clock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	// UTC is still October 26, but Yekaterinburg has already entered October 27.
	*wall, _ = time.Parse(time.RFC3339, "2026-10-26T19:01:00Z")
	if e = service.SyncClock(ctx, "2026-10-26", false); e != nil {
		t.Fatal(e)
	}
	c, e := service.Clock(ctx, "2026-10-26")
	if e != nil || c.State != "ENDED" || c.ModelNow != "2026-10-26T23:59:59+05:00" {
		t.Fatal("midnight handling wrong", c, e)
	}
	day, e := service.CreateDayMode(ctx, "2026-10-27", "LIVE")
	if e != nil || string(day.Now) != "2026-10-27T00:01:00+05:00" {
		t.Fatal("live creation not in server timezone", day.Now, e)
	}
	copy, e := service.CopyDay(ctx, "2026-10-27", "2026-10-28")
	if e != nil {
		t.Fatal(e)
	}
	c, e = service.Clock(ctx, copy.Date)
	if e != nil || c.Mode != "DEMO" {
		t.Fatal("copy unexpectedly started live clock", c, e)
	}
}
func TestClockInvalidTransitionsRollbackAndConcurrentRetry(t *testing.T) {
	x, s, wall := clockSetup(t, "2026-10-27T08:00:00+05:00")
	ctx := context.Background()
	input := modeInput(t, x, "LIVE", "not-today")
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 409)
	*wall, _ = time.Parse(time.RFC3339, "2026-10-26T08:00:00+05:00")
	input.RequestID = "ahead"
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 409)
	*wall = wall.Add(time.Hour)
	input.RequestID = "rollback-mode"
	before := assignmentSnapshot(t, x)
	clockBefore, _ := s.Store.Clock(ctx, "2026-10-26")
	_, e := x.f.store.Pool.Exec(ctx, `CREATE FUNCTION reject_clock_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected clock failure'; END $$; CREATE TRIGGER reject_clock_change BEFORE INSERT ON clock_changes FOR EACH ROW EXECUTE FUNCTION reject_clock_change()`)
	if e != nil {
		t.Fatal(e)
	}
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", input, 500)
	clockAfter, _ := s.Store.Clock(ctx, "2026-10-26")
	if !reflect.DeepEqual(before, assignmentSnapshot(t, x)) || !reflect.DeepEqual(clockBefore, clockAfter) {
		t.Fatal("failed clock switch partially committed")
	}
	if _, e = x.f.store.Pool.Exec(ctx, "DROP TRIGGER reject_clock_change ON clock_changes"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 2)
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("PATCH", assignmentDay+"/clock", bytes.NewReader(encode(input)))
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
	if !bytes.Equal(responses[0].Body.Bytes(), responses[1].Body.Bytes()) {
		t.Fatal("concurrent mode retry differs")
	}
	conflict := input
	conflict.Mode = "DEMO"
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", conflict, 409)
	stale := input
	stale.RequestID = "stale-mode"
	stale.Mode = "DEMO"
	x.call("dispatcher", "PATCH", assignmentDay+"/clock", stale, 409)
}
