package utils

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

type ErrorStruct struct{}

var Error = ErrorStruct{}

func wrapDBErr(err error, message string) error {
	if err != nil {
		return errors.Wrap(err, "DB: "+message)
	}
	return err
}

// ErrNotFound describes the error that we did not find a db object
func (e ErrorStruct) ErrNotFound(err error, what string) error {
	return wrapDBErr(err, "Could not find "+what)
}

type FieldError struct {
	// This represents an error code. While the message of the error may
	// change, the error code will stay the same. All these codes will be
	// included in the documentation with a more decsriptive text.
	ErrorCode int `json:"code"`
	// A human-readable message, describing the error.
	Message string `json:"message"`
	// The field name or id that this error is related to.
	Field string `json:"field"`
	// It is a more detailed error message.
	Detail string `json:"detail"`
}
type ErrorJson struct {
	// This represents an error code. While the message of the error may
	// change, the error code will stay the same. All these codes will be
	// included in the documentation with a more decsriptive text.
	ErrorCode int `json:"code"`
	// A human-readable message, describing the error.
	Message string `json:"message"`
	// The Errors stores the error and its detail of each field.
	Errors []FieldError `json:"errors"`
}

func NewError(errAttrs []FieldError, errorCode int, message error) error {
	fieldErrors := []FieldError{}
	if len(errAttrs) > 0 {
		for _, v := range errAttrs {
			fieldError := FieldError{}
			fieldError.ErrorCode = v.ErrorCode
			fieldError.Message = v.Message
			fieldError.Field = v.Field
			fieldError.Detail = v.Detail
			fieldErrors = append(fieldErrors, fieldError)
		}
	}
	err := ErrorJson{}
	err.ErrorCode = errorCode
	err.Errors = fieldErrors
	err.Message = message.Error()
	return &err
}

func (e *ErrorJson) Error() string {
	var val_err string
	// Write per line field errors.
	if len(e.Errors) > 0 {
		for _, v := range e.Errors {
			val_err = fmt.Sprintln(val_err, "")
			val_err = fmt.Sprintln(val_err, "CODE:", v.ErrorCode)
			val_err = fmt.Sprintln(val_err, "FIELD:", v.Field)
			val_err = fmt.Sprintln(val_err, "MESSAGE:", v.Message)
			val_err = fmt.Sprintln(val_err, "DETAIL:", v.Detail)
		}
	}
	// Lastly, write the general error.
	val_err = fmt.Sprintln(val_err, "GENERAL CODE:", e.ErrorCode)
	val_err = fmt.Sprintln(val_err, "GENERAL MESSAGE:", e.Message)

	return val_err
}

func HttpError(w http.ResponseWriter, status int, str_errors ...error) {
	// Write the HTTP Status on header.
	w.WriteHeader(status)
	if len(str_errors) > 0 {
		// Check if it is ErrorJson type, serialize json and send to writer.
		errj, ok := str_errors[0].(*ErrorJson)
		if ok {
			b, _ := json.Marshal(errj)
			w.Write(b)
			return
		}
		// If not an errorJson type, set errors in a string array and send to writer. To be removed
		// when there are no str_errors with that type.
		serrs := []string{}
		for _, v := range str_errors {
			if v != nil {
				serrs = append(serrs, v.Error())
			}
		}
		errs := map[string]interface{}{
			"errors": serrs,
			"result": "FAILURE",
		}
		b, _ := json.Marshal(errs)
		w.Write(b)
	}
}

func CreateFieldError(errCode int, msg string, field string, detailMsg string) FieldError {
	fieldErr := FieldError{}
	fieldErr.ErrorCode = errCode
	fieldErr.Field = field
	fieldErr.Message = msg
	fieldErr.Detail = detailMsg

	return fieldErr
}
