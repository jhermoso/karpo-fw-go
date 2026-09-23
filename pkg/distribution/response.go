package distribution

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// ProblemDetails represents an RFC 7807 compliant error response.
type ProblemDetails struct {
	Type   string `json:"type,omitempty"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// WriteResult serializes a result.Result[T] into an HTTP JSON response.
// If the result succeeded, it writes successCode and the JSON payload.
// If failed, it derives the appropriate HTTP status code (400, 404, 500) and formats ProblemDetails.
func WriteResult[T any](w http.ResponseWriter, _ *http.Request, res result.Result[T], successCode int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if res.IsSuccess() {
		if successCode <= 0 {
			successCode = http.StatusOK
		}
		w.WriteHeader(successCode)
		_ = json.NewEncoder(w).Encode(res.MustValue())
		return
	}

	err := res.Error()
	errStr := err.Error()

	statusCode := http.StatusInternalServerError
	title := "Internal Server Error"

	errLower := strings.ToLower(errStr)
	switch {
	case strings.Contains(errLower, "not found"):
		statusCode = http.StatusNotFound
		title = "Resource Not Found"
	case strings.Contains(errLower, "required"), strings.Contains(errLower, "invalid"), strings.Contains(errLower, "validation"):
		statusCode = http.StatusBadRequest
		title = "Bad Request"
	case strings.Contains(errLower, "unauthorized"):
		statusCode = http.StatusUnauthorized
		title = "Unauthorized"
	case strings.Contains(errLower, "forbidden"):
		statusCode = http.StatusForbidden
		title = "Forbidden"
	case strings.Contains(errLower, "conflict"), strings.Contains(errLower, "already exists"):
		statusCode = http.StatusConflict
		title = "Conflict"
	}

	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ProblemDetails{
		Title:  title,
		Status: statusCode,
		Detail: errStr,
	})
}
