package handler

import (
	"net/http"

	"github.com/Pertsaa/go-radio/internal/static"
)

func (h *AppHandler) IndexHandler(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "text/html")
	w.Write(static.IndexHTML)
	return nil
}

func (h *AppHandler) CSSHandler(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "text/css")
	w.Write(static.IndexCSS)
	return nil
}

func (h *AppHandler) FaviconHandler(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "image/png")
	w.Write(static.Favicon)
	return nil
}
