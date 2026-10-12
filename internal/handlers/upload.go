package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const catboxUploadURL = "https://catbox.moe/user/api.php"
const maxUploadSize = 10 << 20 // 10 MB
const uploadTimeout = 30 * time.Second

// uploadHTTPClient has explicit timeouts so a hung storage service can't
// stall the request until the platform kills it.
var uploadHTTPClient = &http.Client{
	Timeout: uploadTimeout,
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	},
}

// uploadErrorJSON writes a JSON error body with a user-friendly message.
func uploadErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// isTimeoutError reports whether err is a client-side timeout or a
// context-deadline cancellation (as opposed to the service rejecting us).
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// UploadImage is the server-side proxy for the image storage service (catbox.moe).
// Receives a multipart file (field "image") and forwards it to catbox.
// Returns JSON {"url": "..."} with the direct image URL.
// All error messages are user-friendly (Galician) and include details.
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	// Limit request size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		uploadErrorJSON(w, http.StatusBadRequest,
			"A imaxe é demasiado grande (máx. 10 MB). Reduce o tamaño e tenta de novo.")
		return
	}

	file, fileHeader, err := r.FormFile("image")
	if err != nil {
		uploadErrorJSON(w, http.StatusBadRequest,
			"Nom chegou ningunha imaxe ao servidor. Tenta subila de novo.")
		return
	}
	defer file.Close()

	// Verify file size
	if fileHeader.Size > maxUploadSize {
		uploadErrorJSON(w, http.StatusBadRequest,
			fmt.Sprintf("A imaxe é demasiado grande (%d MB; máx. 10 MB). Reduce o tamaño e tenta de novo.",
				fileHeader.Size>>20))
		return
	}

	// Verify the file looks like an image
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		uploadErrorJSON(w, http.StatusBadRequest,
			fmt.Sprintf("O ficheiro nom parece unha imaxe (tipo «%s»). Escolle un ficheiro de imaxe (JPG, PNG, GIF, WebP…).", contentType))
		return
	}

	// Read the image bytes
	imgBytes, err := io.ReadAll(file)
	if err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Nom foi possível ler o ficheiro. Tenta con outra imaxe.")
		return
	}

	// Build the multipart body catbox expects: reqtype=fileupload + fileToUpload
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("reqtype", "fileupload"); err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Erro interno ao preparar a subida. Tenta de novo.")
		return
	}
	part, err := mw.CreateFormFile("fileToUpload", fileHeader.Filename)
	if err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Erro interno ao preparar a subida. Tenta de novo.")
		return
	}
	if _, err := part.Write(imgBytes); err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Erro interno ao preparar a subida. Tenta de novo.")
		return
	}
	if err := mw.Close(); err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Erro interno ao preparar a subida. Tenta de novo.")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, catboxUploadURL, &body)
	if err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Erro interno ao preparar a subida. Tenta de novo.")
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := uploadHTTPClient.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			uploadErrorJSON(w, http.StatusBadGateway,
				"O servizo de imaxes demorouse demasiado en responder. Tenta de novo nuns segundos.")
			return
		}
		uploadErrorJSON(w, http.StatusBadGateway,
			"Nom foi possível contactar co servizo de almacenamento de imaxes. Verifica a tua conexom e tenta de novo.")
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		uploadErrorJSON(w, http.StatusBadGateway,
			"O servizo de imaxes respondeu de forma inesperada. Tenta de novo nuns segundos.")
		return
	}

	// catbox returns the direct URL as plain text; anything else is an error message
	imageURL := strings.TrimSpace(string(respBody))
	if !strings.HasPrefix(imageURL, "https://") {
		detail := imageURL
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if len(detail) > 120 {
			detail = detail[:120] + "…"
		}
		uploadErrorJSON(w, http.StatusBadGateway,
			fmt.Sprintf("O servizo rexeitou a imaxe: %s. Se o problema persistir, pega a URL dunha imaxe xa alojada noutro sitio.", detail))
		return
	}
	if _, err := url.Parse(imageURL); err != nil {
		uploadErrorJSON(w, http.StatusBadGateway,
			"O servizo respondeu cunha URL inválida. Tenta de novo nuns segundos.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": imageURL})
}
