package handler

import "net/http"

const maxUploadSize = 100 << 20 // 100 MiB per file

// File contents have their own limit; multipart boundaries and metadata
// need extra room within the whole-request cap.
const maxUploadBodySize = maxUploadSize + (1 << 20)

func parseUploadForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodySize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		return err
	}
	for _, header := range r.MultipartForm.File["file"] {
		if header.Size > maxUploadSize {
			return &http.MaxBytesError{Limit: maxUploadSize}
		}
	}
	return nil
}
