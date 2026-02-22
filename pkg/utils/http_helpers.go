package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
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
