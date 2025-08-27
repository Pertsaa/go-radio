package handler

import (
	"net/http"
)

func (h *APIHandler) RadioChannelListHandler(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, h.radio.GetChannels())
}

func (h *APIHandler) RadioChannelStreamHandler(w http.ResponseWriter, r *http.Request) error {
	channelID := r.PathValue("channelID")

	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Content-Type", "audio/mpeg")

	err := h.radio.WriteBuffer(w, channelID)
	if err != nil {
		return err
	}

	err = h.radio.StreamChunks(w, channelID)
	if err != nil {
		return err
	}

	return nil
}
