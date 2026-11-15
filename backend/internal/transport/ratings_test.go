package transport

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRatingRequiresOwnedCompletedVisitAndCountsOnce(t *testing.T) {
	x, id, visit := journeyEmptyCase(t)
	path := "/ui/v1/requests/" + journeyRequests(t, x)[0].ID + "/rating"
	x.call("subscriber", "POST", path, map[string]int{"score": 5}, 403)
	start, _ := time.Parse(time.RFC3339, string(visit.Start))
	journeyTick(t, x, "rating-start", start)
	execute := assignmentDay + "/orders/" + id + "/execution"
	x.call("technician", "POST", execute, map[string]string{"action": "START"}, 200)
	x.call("technician", "POST", execute, map[string]string{"action": "COMPLETE", "report": "Проверено подключение"}, 200)
	x.call("subscriber-two", "POST", path, map[string]int{"score": 5}, 403)
	x.call("technician", "POST", path, map[string]int{"score": 5}, 403)
	x.call("subscriber", "POST", path, map[string]int{"score": 6}, 422)
	for i := 0; i < 2; i++ {
		x.call("subscriber", "POST", path, map[string]int{"score": 4}, 200)
	}
	x.call("subscriber", "POST", path, map[string]int{"score": 3}, 409)
	d := journeyWorkspace(t, x, "dispatcher")
	if d.Ratings["ST-001"].Count != 1 || d.Ratings["ST-001"].Average != 4 {
		t.Fatal("rating was duplicated or incorrect", d.Ratings)
	}
	if len(journeyWorkspace(t, x, "technician-two").Ratings) != 0 {
		t.Fatal("foreign ratings leaked")
	}
	var requests []ServiceRequest
	json.Unmarshal(x.call("subscriber", "GET", "/ui/v1/requests", nil, 200).Body.Bytes(), &requests)
	if requests[0].Order.Rating != 4 {
		t.Fatal("saved rating missing")
	}
}
