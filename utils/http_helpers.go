package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
)

func FilterArrayParams(r *http.Request, b []byte) ([]byte, error) {
	vals := r.URL.Query()
	filter := vals.Get("filter")
	if len(filter) == 0 {
		return b, nil
	}
	words := strings.Split(filter, ",")
	prev_json := []map[string]interface{}{}
	json.Unmarshal(b, &prev_json)
	new_json := []map[string]interface{}{}
	for _, pjs := range prev_json {
		njs := map[string]interface{}{}
		for _, v := range words {
			tmp, ok := pjs[v]
			if ok {
				njs[v] = tmp
			}
		}
		new_json = append(new_json, njs)
	}
	bs, err := json.Marshal(new_json)
	return bs, err
}

func GetUintParam(r *http.Request, param string) (uint, error) {
	sid, ok := mux.Vars(r)[param]
	if !ok {
		return 0, errors.New(ParameterNotExistsErr(param))
	}
	id, err := strconv.ParseUint(sid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

func GetUUIDParam(r *http.Request, param string) (uuid.UUID, error) {
	sid, ok := mux.Vars(r)[param]
	if !ok {
		return uuid.Nil, errors.New(ParameterNotExistsErr(param))
	}

	uid, err := uuid.Parse(sid)
	if err != nil {
		return uuid.Nil, errors.New(ParameterNotUuidErr(param))
	}
	return uid, nil
}

// get uuid query parameter
func GetUUIDQueryParam(r *http.Request, param string) (uuid.UUID, error) {
	sid, ok := r.URL.Query()[param]
	if !ok {
		return uuid.Nil, errors.New(ParameterNotExistsErr(param))
	}

	uid, err := uuid.Parse(sid[0])
	if err != nil {
		return uuid.Nil, errors.New(ParameterNotUuidErr(param))
	}
	return uid, nil
}

func GetStringParam(r *http.Request, param string) (string, error) {
	sid, ok := mux.Vars(r)[param]
	if !ok {
		return "", errors.New(ParameterNotExistsErr(param))
	}
	return sid, nil
}

func GetIntParam(r *http.Request, param string) (int, error) {
	sid, ok := mux.Vars(r)[param]
	if !ok {
		return 0, errors.New(ParameterNotExistsErr(param))
	}
	id, err := strconv.Atoi(sid)
	if err != nil {
		return 0, err
	}
	return id, nil
}

var CloseConnectionMiddleware = func(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Clear all contents on the given request.
		defer context.Clear(r)
		// Close connection after request finished.
		w.Header().Set("Connection", "close")
		// The content data is in JSON format.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", r.Header.Get("request_id"))
		// Go to the next handler.
		next.ServeHTTP(w, r)
	})
}

func IsSuccessfulStatusCode(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func GetStringQueryParam(r *http.Request, param string) (string, error) {
	values := r.URL.Query()
	value := values.Get(param)
	if value == "" {
		return "", errors.New(ParameterNotExistsErr(param))
	}
	return value, nil
}

func GetIntQueryParam(r *http.Request, param string) (int, error) {
	strValue, err := GetStringQueryParam(r, param)
	if err != nil {
		return 0, err
	}
	intValue, err := strconv.Atoi(strValue)
	if err != nil {
		return 0, errors.New(ParameterNotExistsErr(param))
	}
	return intValue, nil
}

// Middleware to set Content-Type as JSON
func JsonContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// Middleware to allow CORS from all origins
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
