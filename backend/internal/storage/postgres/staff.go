package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"magistral/backend/internal/dto"
)

func ApplyStaffOverrides(r *dto.ReferenceData, overrides map[string]dto.Staff) {
	for i, person := range r.Staff {
		if replacement, ok := overrides[person.StaffId]; ok {
			r.Staff[i] = replacement
		}
	}
}

type StaffVisitChange struct {
	OrderID     string `json:"order_id"`
	BeforeStaff string `json:"before_staff,omitempty"`
	AfterStaff  string `json:"after_staff,omitempty"`
	BeforeStart string `json:"before_start,omitempty"`
	BeforeEnd   string `json:"before_end,omitempty"`
	AfterStart  string `json:"after_start,omitempty"`
	AfterEnd    string `json:"after_end,omitempty"`
	Reason      string `json:"reason,omitempty"`
}
type StaffChangeRecord struct {
	RequestID    string             `json:"request_id"`
	StaffID      string             `json:"staff_id"`
	ActorID      string             `json:"actor_id"`
	ActorName    string             `json:"actor_name"`
	Reason       string             `json:"reason"`
	AsOf         string             `json:"as_of"`
	PlanVersion  int64              `json:"plan_version"`
	Revision     int64              `json:"revision"`
	Before       dto.Staff          `json:"before"`
	After        dto.Staff          `json:"after"`
	AbsentBefore bool               `json:"absent_before"`
	AbsentAfter  bool               `json:"absent_after"`
	Changes      []StaffVisitChange `json:"changes"`
}

func (s *Store) StaffChangeDuplicate(ctx context.Context, date, id string) (StaffChangeRecord, string, error) {
	var record StaffChangeRecord
	var raw []byte
	var hash string
	e := s.Pool.QueryRow(ctx, `SELECT c.document,c.payload_hash FROM staff_changes c JOIN days d ON d.date=c.day AND d.generation=c.generation WHERE d.date::text=$1 AND c.request_id=$2`, date, id).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return record, "", nil
	}
	if e != nil {
		return record, "", e
	}
	e = json.Unmarshal(raw, &record)
	return record, hash, e
}
func (s *Store) PublishStaffChange(ctx context.Context, in EventInputs, person dto.Staff, day dto.DayInput, plan *dto.Plan, record StaffChangeRecord, hash string) error {
	return s.Update(ctx, func(tx pgx.Tx) error {
		var gen, rev, rv, mv int64
		e := tx.QueryRow(ctx, `SELECT d.generation,d.plan_revision,r.version,m.version FROM days d CROSS JOIN reference_data r CROSS JOIN travel_matrices m WHERE d.date::text=$1`, day.Date).Scan(&gen, &rev, &rv, &mv)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrStale
		}
		if e != nil {
			return e
		}
		if gen != in.Generation || rev != in.Revision || rv != in.ReferenceVersion || mv != in.MatrixVersion {
			return ErrStale
		}
		overrides := in.StaffOverrides
		if overrides == nil {
			overrides = map[string]dto.Staff{}
		}
		overrides[person.StaffId] = person
		overrideRaw, e := json.Marshal(overrides)
		if e != nil {
			return e
		}
		dayRaw, e := json.Marshal(day)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		version := in.Plan.PlanVersion
		if plan != nil {
			version = plan.PlanVersion
			planRaw, e := json.Marshal(plan)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO plan_snapshots(day,generation,revision,plan_version,reference_version,matrix_version,document) VALUES($1,$2,$3,$4,$5,$6,$7)`, day.Date, gen, rev+1, version, rv, mv, planRaw); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, `INSERT INTO staff_changes(day,generation,request_id,payload_hash,revision,document) VALUES($1,$2,$3,$4,$5,$6)`, day.Date, gen, record.RequestID, hash, rev+1, raw); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE days SET document=$2,staff_overrides=$3,plan_revision=plan_revision+1,current_version=$4,staff_change_count=staff_change_count+1 WHERE date::text=$1`, day.Date, dayRaw, overrideRaw, version)
		return e
	})
}
