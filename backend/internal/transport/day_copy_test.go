package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"magistral/backend/internal/application"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/storage/postgres"
	"testing"
	"time"
)

func TestCopyShiftMovesDatesWithoutReplacingHistory(t *testing.T) {
	c := accessSetup(t)
	source := "/ui/v1/days/2026-10-26/workspace"
	before := c.call("dispatcher", "GET", source, nil, 200).Body.Bytes()
	path := "/ui/v1/days/2026-10-26/copy"
	input := map[string]string{"date": "2026-11-16"}
	c.call("technician", "POST", path, input, 403)
	c.call("subscriber", "POST", path, input, 403)
	c.call("dispatcher", "POST", path, map[string]string{"date": "2026-10-26"}, 400)
	c.call("dispatcher", "POST", path, input, 201)
	c.call("dispatcher", "POST", path, input, 409)
	c.call("dispatcher", "POST", "/ui/v1/days/2025-01-01/copy", map[string]string{"date": "2026-11-17"}, 404)
	var original, copy postgres.Workspace
	json.Unmarshal(before, &original)
	json.Unmarshal(c.call("dispatcher", "GET", "/ui/v1/days/2026-11-16/workspace", nil, 200).Body.Bytes(), &copy)
	if len(copy.Day.Orders) != len(original.Day.Orders) || copy.Plan != nil || copy.EventCount != 0 || len(copy.Facts) != 0 || copy.Generation == original.Generation {
		t.Fatal("copy did not create a fresh scenario")
	}
	offset := 21 * 24 * time.Hour
	for i, o := range copy.Day.Orders {
		old := original.Day.Orders[i]
		for _, pair := range [][2]dto.DateTime{{old.RegisteredAt, o.RegisteredAt}, {old.Slot.Start, o.Slot.Start}, {old.Slot.End, o.Slot.End}, {old.Deadline, o.Deadline}, {old.LatestStart, o.LatestStart}} {
			from, _ := time.Parse(time.RFC3339, string(pair[0]))
			to, _ := time.Parse(time.RFC3339, string(pair[1]))
			if to.Sub(from) != offset {
				t.Fatal("a date was not moved consistently")
			}
		}
		if o.OrderId == old.OrderId {
			t.Fatal("order ID kept the source date")
		}
	}
	c.call("dispatcher", "POST", "/api/v1/days/2026-11-16/plan", nil, 200)
	if !bytes.Equal(before, c.call("dispatcher", "GET", source, nil, 200).Body.Bytes()) {
		t.Fatal("source history changed")
	}
	var days []postgres.DaySummary
	json.Unmarshal(c.call("dispatcher", "GET", "/ui/v1/days", nil, 200).Body.Bytes(), &days)
	if days[0].SourceDate != "2026-10-26" || days[0].OrdersTotal != 2 || days[0].StaffTotal != 2 {
		t.Fatal("copy metadata/counts missing")
	}
}

func TestDirectNewOrderRejectsWrongBuildingNorm(t *testing.T) {
	c := accessSetup(t)
	ctx := context.Background()
	in, e := c.f.store.EventInputs(ctx, "2026-10-26")
	if e != nil {
		t.Fatal(e)
	}
	reference := in.Reference
	private := reference.WorkTypes[1]
	private.Code = "INST_PRIVATE_VLS"
	reference.WorkTypes = append(reference.WorkTypes, private)
	service := application.Service{Store: c.f.store}
	if e = service.Reference(ctx, reference); e != nil {
		t.Fatal(e)
	}
	if e = service.Day(ctx, in.Day.Date, in.Day); e != nil {
		t.Fatal(e)
	}
	if _, e = service.BuildPlan(ctx, in.Day.Date, 3); e != nil {
		t.Fatal(e)
	}
	before := c.call("dispatcher", "GET", "/ui/v1/days/2026-10-26/workspace", nil, 200).Body.Bytes()
	order := in.Day.Orders[0]
	order.OrderId = "ORD-20261026-0401"
	order.WorkTypeCode = "INST_PRIVATE_VLS"
	c.call("dispatcher", "POST", "/api/v1/days/2026-10-26/events", dto.EventInput{EventId: "bad-private", Type: "NEW_ORDER", Timestamp: in.Day.Now, Order: &order}, 422)
	if !bytes.Equal(before, c.call("dispatcher", "GET", "/ui/v1/days/2026-10-26/workspace", nil, 200).Body.Bytes()) {
		t.Fatal("invalid order changed data")
	}
}
