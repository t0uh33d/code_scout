package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/utils"
)

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func HTTPStatusForAppError(code int) int {
	switch code {
	case domain.AUTHORIZATION_HEADER_MISSING,
		domain.UNAUTHORIZED:
		return http.StatusUnauthorized
	case domain.INVALID_REQUEST_DATA_ERR_CODE:
		return http.StatusBadRequest
	case domain.ERR_PAYLOAD_TOO_LARGE_ERR_CODE:
		return http.StatusRequestEntityTooLarge
	case domain.ERR_DAILY_CAP_REACHED_ERR_CODE:
		return http.StatusTooManyRequests
	case domain.ERR_INVALID_PROJECT_NAME_ERR_CODE:
		return http.StatusBadRequest
	case domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE:
		return http.StatusInternalServerError
	case domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE:
		return http.StatusInternalServerError
	case domain.PROJECT_ID_HEADER_MISSING_ERR_CODE,
		domain.PROJECT_SECRET_HEADER_MISSING_ERR_CODE:
		return http.StatusBadRequest
	case domain.INVALID_PROJECT_ID_HEADER_ERR_CODE,
		domain.INVALID_PROJECT_SECRET_HEADER_ERR_CODE:
		return http.StatusBadRequest
	case domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
		domain.ERR_PROJECT_SECRET_MISSING_ERR_CODE:
		return http.StatusNotFound
	case domain.ERR_FAILED_TO_UPDATE_PROJECT_ERR_CODE,
		domain.ERR_FAILED_TO_ROTATE_SECRET_ERR_CODE:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func WriteError(w http.ResponseWriter, err error) {
	var errJson *utils.ErrorJson
	if errors.As(err, &errJson) {
		status := HTTPStatusForAppError(errJson.ErrorCode)
		// Set before WriteHeader, or the header never reaches the wire.
		// Delta-seconds, never the HTTP-date form: a phone's clock is
		// routinely wrong and the date form would make the pause depend on
		// the client's skew.
		if errJson.RetryAfterSeconds > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(errJson.RetryAfterSeconds))
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(errJson)
		return
	}

	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(ErrorResponse{
		Code:    0,
		Message: "Internal server error",
	})
}
