package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const catboxUploadURL = "https://catbox.moe/user/api.php"
const imgbbUploadURL = "https://api.imgbb.com/1/upload"
const imgbbAPIKey = "2af9b79fe6bb155e02344bf3185e8196"

const maxUploadSize = 10 << 20 // 10 MB
const uploadTimeout = 25 * time.Second

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

// isTimeoutError reports whether err is a client-side timeout or a
// context-deadline cancellation.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// sanitizeUploadFilename ensures the file passed to the external service
// has a clean basename and a recognized image extension.
func sanitizeUploadFilename(orig string, mimeType string) string {
	base := filepath.Base(orig)
	ext := strings.ToLower(filepath.Ext(base))

	if ext == "" || ext == "." {
		switch {
		case strings.Contains(mimeType, "png"):
			ext = ".png"
		case strings.Contains(mimeType, "webp"):
			ext = ".webp"
		case strings.Contains(mimeType, "gif"):
			ext = ".gif"
		default:
			ext = ".jpg"
		}
	}

	cleanName := strings.TrimSuffix(base, filepath.Ext(base))
	if cleanName == "" || cleanName == "." {
		cleanName = "upload"
	}
	return cleanName + ext
}

// uploadToCatbox uploads an image to catbox.moe.
func uploadToCatbox(ctx context.Context, filename string, imgBytes []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("reqtype", "fileupload"); err != nil {
		return "", fmt.Errorf("catbox: failed to write reqtype: %w", err)
	}
	part, err := mw.CreateFormFile("fileToUpload", filename)
	if err != nil {
		return "", fmt.Errorf("catbox: failed to create form file: %w", err)
	}
	if _, err := part.Write(imgBytes); err != nil {
		return "", fmt.Errorf("catbox: failed to write file bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("catbox: failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, catboxUploadURL, &body)
	if err != nil {
		return "", fmt.Errorf("catbox: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")

	resp, err := uploadHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("catbox: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return "", fmt.Errorf("catbox: failed to read response body: %w", err)
	}

	imageURL := strings.TrimSpace(string(respBody))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(imageURL, "https://") {
		return "", fmt.Errorf("catbox rejected upload (status %d): %s", resp.StatusCode, imageURL)
	}
	if _, err := url.Parse(imageURL); err != nil {
		return "", fmt.Errorf("catbox returned invalid URL (%s): %w", imageURL, err)
	}

	return imageURL, nil
}

// uploadToIMGBB uploads an image to IMGBB as a robust secondary provider.
func uploadToIMGBB(ctx context.Context, filename string, imgBytes []byte) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	formData := url.Values{}
	formData.Set("key", imgbbAPIKey)
	formData.Set("image", encoded)
	formData.Set("name", filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, imgbbUploadURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("imgbb: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := uploadHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("imgbb: request failed: %w", err)
	}
	defer resp.Body.Close()

	var imgbbResp imgbbResponse
	if err := json.NewDecoder(resp.Body).Decode(&imgbbResp); err != nil {
		return "", fmt.Errorf("imgbb: failed to decode response (status %d): %w", resp.StatusCode, err)
	}

	if !imgbbResp.Success {
		return "", fmt.Errorf("imgbb rejected upload (status %d): %s", imgbbResp.Status, imgbbResp.Error.Message)
	}

	imageURL := imgbbResp.Data.DisplayURL
	if imageURL == "" {
		imageURL = imgbbResp.Data.Image.URL
	}
	if imageURL == "" {
		return "", errors.New("imgbb: response did not include an image URL")
	}

	return imageURL, nil
}

// UploadImage is the server-side proxy for image storage.
// Receives a multipart file (field "image") and uploads to Catbox with IMGBB fallback.
// Returns JSON {"url": "..."} with the direct image URL.
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	// Limit request size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		uploadErrorJSON(w, http.StatusBadRequest,
			"A imagem é demasiado grande (máx. 10 MB). Reduce o tamanho e tenta de novo.")
		return
	}

	file, fileHeader, err := r.FormFile("image")
	if err != nil {
		uploadErrorJSON(w, http.StatusBadRequest,
			"Não chegou nenhuma imagem ao servidor. Tenta enviá-la de novo.")
		return
	}
	defer file.Close()

	// Verify file size
	if fileHeader.Size > maxUploadSize {
		uploadErrorJSON(w, http.StatusBadRequest,
			fmt.Sprintf("A imagem é demasiado grande (%d MB; máx. 10 MB). Reduce o tamanho e tenta de novo.",
				fileHeader.Size>>20))
		return
	}

	// Read the image bytes
	imgBytes, err := io.ReadAll(file)
	if err != nil {
		uploadErrorJSON(w, http.StatusInternalServerError,
			"Não foi possível ler o ficheiro. Tenta con outra imagem.")
		return
	}

	// Verify content type (use DetectContentType if missing or generic)
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(imgBytes)
	}
	if !strings.HasPrefix(contentType, "image/") {
		uploadErrorJSON(w, http.StatusBadRequest,
			fmt.Sprintf("O ficheiro não parece uma imagem (tipo «%s»). Escolhe um ficheiro de imagem (JPG, PNG, GIF, WebP…).", contentType))
		return
	}

	filename := sanitizeUploadFilename(fileHeader.Filename, contentType)

	// Try Catbox first
	imageURL, catboxErr := uploadToCatbox(r.Context(), filename, imgBytes)
	if catboxErr != nil {
		log.Printf("UploadImage: Catbox upload failed (%v), attempting IMGBB fallback...", catboxErr)

		// Fallback to IMGBB
		var imgbbErr error
		imageURL, imgbbErr = uploadToIMGBB(r.Context(), filename, imgBytes)
		if imgbbErr != nil {
			log.Printf("UploadImage: IMGBB fallback failed as well (%v)", imgbbErr)

			if isTimeoutError(catboxErr) || isTimeoutError(imgbbErr) {
				uploadErrorJSON(w, http.StatusBadGateway,
					"O servizo de imagems demorouse demasiado en responder. Tenta de novo nuns segundos.")
				return
			}
			uploadErrorJSON(w, http.StatusBadGateway,
				"Nom foi possível guardar a imagem nos servizos de almacenamento. Tenta de novo em alguns segundos ou utiliza um URL externo.")
			return
		}
		log.Printf("UploadImage: IMGBB fallback succeeded -> %s", imageURL)
	} else {
		log.Printf("UploadImage: Catbox upload succeeded -> %s", imageURL)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": imageURL})
}
