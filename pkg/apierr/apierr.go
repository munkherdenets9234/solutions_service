package apierr

import "net/http"

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

func New(statusCode int, message string) *APIError {
	return &APIError{StatusCode: statusCode, Message: message}
}

func NotFound(msg string) *APIError  { return New(http.StatusNotFound, msg) }
func BadRequest(msg string) *APIError { return New(http.StatusBadRequest, msg) }
func Unauthorized() *APIError         { return New(http.StatusUnauthorized, "unauthorized") }
func Internal() *APIError             { return New(http.StatusInternalServerError, "internal server error") }
