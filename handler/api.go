package handler

import (
	"net/http"
)

func (h *Handler) RadioChannelListHandler(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, h.radio.GetChannels())
}

func (h *Handler) RadioChannelStreamHandler(w http.ResponseWriter, r *http.Request) error {
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

func (h *Handler) RadioFileUploadHandler(w http.ResponseWriter, r *http.Request) error {
	r.ParseMultipartForm(100)

	// for _, fileHeader := range r.MultipartForm.File["files"] {
	// 	file, err := fileHeader.Open()
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer file.Close()

	// 	fileName := fileHeader.Filename
	// 	fileExt := filepath.Ext(fileName)
	// 	fileNameWithoutExt := fileName[:len(fileName)-len(fileExt)]
	// 	lowerCaseExt := strings.ToLower(fileExt)
	// 	newFileName := fileNameWithoutExt + lowerCaseExt

	// 	dst, err := os.Create(filepath.Join(h.radio.AudioDir, newFileName))
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer dst.Close()

	// 	if _, err := io.Copy(dst, file); err != nil {
	// 		return err
	// 	}
	// }

	// h.radio.ScanAudioFiles()

	return writeJSON(w, http.StatusOK, nil)
}
