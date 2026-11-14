package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"magistral/backend/internal/dto"
	"time"
)

type ShiftClock struct {
	Date        string     `json:"date"`
	Generation  int64      `json:"generation"`
	Revision    int64      `json:"revision"`
	PlanVersion int64      `json:"plan_version"`
	Mode        string     `json:"mode"`
	ModelNow    string     `json:"model_now"`
	ServerNow   string     `json:"server_now"`
	State       string     `json:"state"`
	Error       string     `json:"error,omitempty"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
}
type ClockChange struct {
	Revision  int64  `json:"revision"`
	RequestID string `json:"request_id"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
	From      string `json:"from_mode"`
	To        string `json:"to_mode"`
	AsOf      string `json:"as_of"`
	ChangedAt string `json:"changed_at"`
}

func (s *Store) Clock(ctx context.Context, date string) (ShiftClock, error) {
	var out ShiftClock
	e := s.Pool.QueryRow(ctx, `SELECT date::text,generation,clock_revision,current_version,clock_mode,document->>'now',clock_error,clock_synced_at FROM days WHERE date::text=$1`, date).Scan(&out.Date, &out.Generation, &out.Revision, &out.PlanVersion, &out.Mode, &out.ModelNow, &out.Error, &out.SyncedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrMissing
	}
	return out, e
}
func (s *Store) ClockChanges(ctx context.Context, date string) ([]ClockChange, error) {
	rows, e := s.Pool.Query(ctx, `SELECT c.document FROM clock_changes c JOIN days d ON d.date=c.day AND d.generation=c.generation WHERE d.date::text=$1 ORDER BY (c.document->>'revision')::bigint`, date)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ClockChange{}
	for rows.Next() {
		var raw []byte
		var record ClockChange
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &record); e != nil {
			return nil, e
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
func (s *Store) ClockDuplicate(ctx context.Context, date, id string) (ClockChange, string, error) {
	var out ClockChange
	var raw []byte
	var hash string
	e := s.Pool.QueryRow(ctx, `SELECT c.document,c.payload_hash FROM clock_changes c JOIN days d ON d.date=c.day AND d.generation=c.generation WHERE d.date::text=$1 AND c.request_id=$2`, date, id).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, "", nil
	}
	if e != nil {
		return out, "", e
	}
	e = json.Unmarshal(raw, &out)
	return out, hash, e
}
func WriteClockMode(ctx context.Context, tx pgx.Tx, date string, generation int64, record ClockChange, hash string) error {
	raw, e := json.Marshal(record)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO clock_changes(day,generation,request_id,payload_hash,document) VALUES($1,$2,$3,$4,$5)`, date, generation, record.RequestID, hash, raw)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE days SET clock_mode=$2,clock_revision=clock_revision+1,clock_error='',clock_synced_at=$3 WHERE date::text=$1`, date, record.To, record.ChangedAt)
	return e
}
func (s *Store) PublishClockWithoutPlan(ctx context.Context, in EventInputs, at dto.DateTime, synced time.Time, hook func(pgx.Tx) error) error {
	return s.Update(ctx, func(tx pgx.Tx) error {
		day := in.Day
		day.Now = at
		raw, e := json.Marshal(day)
		if e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, `UPDATE days SET document=$4,model_now=$5,plan_revision=plan_revision+1,clock_error='',clock_synced_at=$6 WHERE date::text=$1 AND generation=$2 AND plan_revision=$3 AND current_version=0`, day.Date, in.Generation, in.Revision, raw, string(at), synced)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrStale
		}
		if hook != nil {
			return hook(tx)
		}
		return nil
	})
}
func (s *Store) LiveClockDays(ctx context.Context, today string) ([]string, error) {
	rows, e := s.Pool.Query(ctx, `SELECT date::text FROM days WHERE clock_mode='LIVE' AND date::text<=$1 AND (date::text=$1 OR document->>'now'<date::text||'T23:59:59+05:00') ORDER BY date`, today)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var date string
		if e = rows.Scan(&date); e != nil {
			return nil, e
		}
		out = append(out, date)
	}
	return out, rows.Err()
}
