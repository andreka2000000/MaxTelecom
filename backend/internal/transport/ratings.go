package transport

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	"net/http"
)

func (a *API) rateRequest(w http.ResponseWriter, r *http.Request, u auth.User) {
	var input struct {
		Score int `json:"score"`
	}
	if !readBody(w, r, &input) {
		return
	}
	if input.Score < 1 || input.Score > 5 {
		fail(w, &application.Error{Status: 422, Code: "INVALID_RATING", Message: "Выберите оценку от 1 до 5."})
		return
	}
	e := a.Service.Store.Update(r.Context(), func(tx pgx.Tx) error {
		var staff string
		e := tx.QueryRow(r.Context(), `SELECT f.staff_id FROM service_requests r JOIN request_orders l ON l.request_id=r.id JOIN days d ON d.date::text=l.day AND d.generation=l.generation JOIN visit_facts f ON f.day=l.day AND f.generation=l.generation AND f.order_id=l.order_id WHERE r.id=$1 AND r.user_id=$2 AND f.status='COMPLETED'`, r.PathValue("id"), u.ID).Scan(&staff)
		if errors.Is(e, pgx.ErrNoRows) {
			return &application.Error{Status: 403, Code: "RATING_FORBIDDEN", Message: "Оценить можно только свою выполненную заявку."}
		}
		if e != nil {
			return e
		}
		var score int
		e = tx.QueryRow(r.Context(), `INSERT INTO employee_ratings(request_id,staff_id,score) VALUES($1,$2,$3) ON CONFLICT(request_id) DO UPDATE SET score=employee_ratings.score RETURNING score`, r.PathValue("id"), staff, input.Score).Scan(&score)
		if e == nil && score != input.Score {
			return &application.Error{Status: 409, Code: "RATING_EXISTS", Message: "Оценка уже сохранена."}
		}
		return e
	})
	if e != nil {
		fail(w, e)
		return
	}
	write(w, 200, map[string]bool{"saved": true})
}
