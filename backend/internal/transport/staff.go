package transport

import (
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	"net/http"
)

func (a *API) staffRoutes(mux *http.ServeMux, guard func([]string, func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc) {
	mux.HandleFunc("POST /ui/v1/days/{date}/staff/{staff}/preview", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		var input application.StaffEditInput
		if !readBody(w, r, &input) {
			return
		}
		out, e := a.Service.PreviewStaff(r.Context(), r.PathValue("date"), r.PathValue("staff"), input)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
	mux.HandleFunc("PATCH /ui/v1/days/{date}/staff/{staff}", guard([]string{auth.Dispatcher}, func(w http.ResponseWriter, r *http.Request, u auth.User) {
		var input application.StaffEditInput
		if !readBody(w, r, &input) {
			return
		}
		out, e := a.Service.EditStaff(r.Context(), r.PathValue("date"), r.PathValue("staff"), input, u)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, out)
	}))
}
