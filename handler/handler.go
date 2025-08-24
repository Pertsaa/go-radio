package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Pertsaa/go-radio/radio"
)

type Handler struct {
	ctx   context.Context
	radio *radio.Radio
}

func NewHandler(ctx context.Context, radio *radio.Radio) *Handler {
	return &Handler{
		ctx:   ctx,
		radio: radio,
	}
}

func (h *Handler) NotFoundHandler(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, "Not Found")
	return nil
}

type APIError struct {
	Code    int `json:"code"`
	Message any `json:"message"`
}

func (e APIError) Error() string {
	return fmt.Sprintf("%d: %v", e.Code, e.Message)
}

func NewAPIError(code int, message any) APIError {
	return APIError{
		Code:    code,
		Message: message,
	}
}

type APIFunc func(w http.ResponseWriter, r *http.Request) error

func Make(h APIFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			if apiErr, ok := err.(APIError); ok {
				writeJSON(w, apiErr.Code, apiErr)
			} else {
				internalErr := map[string]any{
					"code":    http.StatusInternalServerError,
					"message": "internal server error",
				}
				writeJSON(w, http.StatusInternalServerError, internalErr)
			}
			slog.Error("handler error", "err", err.Error(), "path", r.URL.Path)
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(data)
}

// func parseBody[T any](r *http.Request) (T, error) {
// 	var body T
// 	err := json.NewDecoder(r.Body).Decode(&body)
// 	if err != nil {
// 		return body, err
// 	}
// 	defer r.Body.Close()
// 	return body, nil
// }
