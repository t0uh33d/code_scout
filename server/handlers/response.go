package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/t0uh33d/code_scout/server/middleware"
)

func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func RespondError(w http.ResponseWriter, err error) {
	middleware.WriteError(w, err)
}
