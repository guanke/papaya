package api

import (
	"net/http"
)

func Healthcheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json") // todo: move to middleware

	w.Write([]byte(`{"Status": "OK"}`))
	w.WriteHeader(http.StatusOK)

}
