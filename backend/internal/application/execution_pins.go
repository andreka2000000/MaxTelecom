package application

import "context"

// A persisted departure/arrival/work fact freezes that visit and the route prefix
// leading to it. Model-time progress alone cannot revoke an active assignment.
type executionHistory struct {
	Pins   map[string]bool
	Status map[string]string
}

func (s *Service) executionHistory(ctx context.Context, date string, generation int64) (executionHistory, error) {
	out := executionHistory{Pins: map[string]bool{}, Status: map[string]string{}}
	rows, e := s.Store.Pool.Query(ctx, "SELECT order_id,status FROM visit_facts WHERE day=$1 AND generation=$2", date, generation)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if e = rows.Scan(&id, &status); e != nil {
			return out, e
		}
		out.Pins[id] = true
		out.Status[id] = status
	}
	return out, rows.Err()
}
