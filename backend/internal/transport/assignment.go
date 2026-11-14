package transport

import (
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	"net/http"
)

func (a *API) assignmentRoutes(mux *http.ServeMux, guard func([]string, func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc) {
	mux.HandleFunc("GET /ui/v1/days/{date}/orders/{order}/assignment-options", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		out, e := a.Service.AssignmentPreview(r.Context(), r.PathValue("date"), r.PathValue("order"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		write(w, 200, out)
	}))
	mux.HandleFunc("POST /ui/v1/days/{date}/orders/{order}/assignment", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, u auth.User) {
		var in application.AssignmentInput
		if !readBody(w, r, &in) {
			return
		}
		out, e := a.Service.Assign(r.Context(), r.PathValue("date"), r.PathValue("order"), in, u)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
}
