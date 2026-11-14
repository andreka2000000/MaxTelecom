package transport

import (
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	"net/http"
)

func (a *API) clockRoutes(mux *http.ServeMux, guard func([]string, func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc) {
	mux.HandleFunc("GET /ui/v1/days/{date}/clock", guard([]string{auth.Dispatcher, auth.Technician}, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		out, e := a.Service.Clock(r.Context(), r.PathValue("date"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
	mux.HandleFunc("GET /ui/v1/days/{date}/clock/history", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		out, e := a.Service.Store.ClockChanges(r.Context(), r.PathValue("date"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
	mux.HandleFunc("PATCH /ui/v1/days/{date}/clock", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, u auth.User) {
		var in application.ClockModeInput
		if !readBody(w, r, &in) {
			return
		}
		out, e := a.Service.SetClockMode(r.Context(), r.PathValue("date"), in, u)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
	mux.HandleFunc("POST /ui/v1/days/{date}/clock/sync", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		if e := a.Service.SyncClock(r.Context(), r.PathValue("date"), true); e != nil {
			fail(w, e)
			return
		}
		out, e := a.Service.Clock(r.Context(), r.PathValue("date"))
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
}
