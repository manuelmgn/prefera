package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const imgbbAPIKey = "2af9b79fe6bb155e02344bf3185e8196"
const imgbbUploadURL = "https://api.imgbb.com/1/upload"
const maxUploadSize = 10 << 20 // 10 MB

// imgbbResponse represents the IMGBB API response
type imgbbResponse struct {
	Data struct {
		DisplayURL string `json:"display_url"`
		Image      struct {
			URL string `json:"url"`
		} `json:"image"`
	} `json:"data"`
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
	Success bool `json:"success"`
	Status  int  `json:"status"`
}

// uploadErrorJSON writes a JSON error body with a user-friendly message.
func uploadErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// UploadImage is the server-side proxy for the IMGBB API.
// Receives a multipart file (field "image") and forwards it to IMGBB.
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

	// Base64-encode for IMGBB API
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	// Build the IMGBB request
	formData := url.Values{}
	formData.Set("key", imgbbAPIKey)
	formData.Set("image", encoded)

	resp, err := http.Post(imgbbUploadURL, "application/x-www-form-urlencoded", strings.NewReader(formData.Encode()))
	if err != nil {
		uploadErrorJSON(w, http.StatusBadGateway,
			"Nom foi possível contactar co servizo de almacenamento de imaxes. Verifica a tua conexom e tenta de novo.")
		return
	}
	defer resp.Body.Close()

	var imgbbResp imgbbResponse
	if err := json.NewDecoder(resp.Body).Decode(&imgbbResp); err != nil {
		uploadErrorJSON(w, http.StatusBadGateway,
			"O servizo de imaxes respondeu de forma inesperada. Tenta de novo nuns segundos.")
		return
	}

	if !imgbbResp.Success {
		detail := strings.TrimSpace(imgbbResp.Error.Message)
		if detail == "" {
			detail = fmt.Sprintf("código %d", imgbbResp.Status)
		}
		if len(detail) > 120 {
			detail = detail[:120] + "…"
		}
		uploadErrorJSON(w, http.StatusBadGateway,
			fmt.Sprintf("O servizo rexeitou a imaxe: %s. Se o problema persistir, pega a URL dunha imaxe xa alojada noutro sitio.", detail))
		return
	}

	// Prefer display_url (no expiry) over image.url
	imageURL := imgbbResp.Data.DisplayURL
	if imageURL == "" {
		imageURL = imgbbResp.Data.Image.URL
	}
	if imageURL == "" {
		uploadErrorJSON(w, http.StatusBadGateway,
			"O servizo respondeu sen URL de imaxe. Tenta de novo nuns segundos.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": imageURL})
}
