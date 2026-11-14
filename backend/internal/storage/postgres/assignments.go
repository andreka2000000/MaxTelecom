package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"magistral/backend/internal/dto"
)

type AssignmentRecord struct {
	RequestID   string `json:"request_id"`
	OrderID     string `json:"order_id"`
	ActorID     string `json:"actor_id"`
	ActorName   string `json:"actor_name"`
	FromStaff   string `json:"from_staff,omitempty"`
	ToStaff     string `json:"to_staff"`
	Reason      string `json:"reason"`
	AsOf        string `json:"as_of"`
	PlanVersion int64  `json:"plan_version"`
}

func (s *Store) AssignmentDuplicate(ctx context.Context, date, id string) (AssignmentRecord, string, error) {
	var raw []byte
	var hash string
	var out AssignmentRecord
	e := s.Pool.QueryRow(ctx, `SELECT a.document,a.payload_hash FROM manual_assignments a JOIN days d ON d.date=a.day AND d.generation=a.generation WHERE d.date::text=$1 AND a.request_id=$2`, date, id).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, "", nil
	}
	if e != nil {
		return out, "", e
	}
	e = json.Unmarshal(raw, &out)
	return out, hash, e
}
func (s *Store) PublishAssignment(ctx context.Context, in EventInputs, p dto.Plan, record AssignmentRecord, hash string) error {
	return s.Update(ctx, func(tx pgx.Tx) error {
		var gen, rev, rv, mv int64
		e := tx.QueryRow(ctx, `SELECT d.generation,d.plan_revision,r.version,m.version FROM days d CROSS JOIN reference_data r CROSS JOIN travel_matrices m WHERE d.date::text=$1`, in.Day.Date).Scan(&gen, &rev, &rv, &mv)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrStale
		}
		if e != nil {
			return e
		}
		if gen != in.Generation || rev != in.Revision || rv != in.ReferenceVersion || mv != in.MatrixVersion {
			return ErrStale
		}
		planRaw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO plan_snapshots(day,generation,revision,plan_version,reference_version,matrix_version,document) VALUES($1,$2,$3,$4,$5,$6,$7)`, in.Day.Date, gen, rev+1, p.PlanVersion, rv, mv, planRaw); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO manual_assignments(day,generation,request_id,payload_hash,plan_version,document) VALUES($1,$2,$3,$4,$5,$6)`, in.Day.Date, gen, record.RequestID, hash, p.PlanVersion, raw); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE days SET plan_revision=plan_revision+1,current_version=$2,assignment_count=assignment_count+1 WHERE date::text=$1`, in.Day.Date, p.PlanVersion)
		return e
	})
}
