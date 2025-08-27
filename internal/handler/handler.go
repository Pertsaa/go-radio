package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Pertsaa/go-radio/internal/radio"
)

type APIHandler struct {
	ctx   context.Context
	radio *radio.Radio
}

func NewAPIHandler(ctx context.Context, radio *radio.Radio) *APIHandler {
	return &APIHandler{
		ctx:   ctx,
		radio: radio,
	}
}

type AppHandler struct {
	ctx context.Context
}

func NewAppHandler(ctx context.Context) *AppHandler {
	return &AppHandler{
		ctx: ctx,
	}
}

func (h *APIHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, "Not Found")
	return nil
}

func (h *AppHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) error {
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
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(data)
}
