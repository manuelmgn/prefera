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

// imgbbResponse representa a resposta da API do IMGBB
type imgbbResponse struct {
	Data struct {
		DisplayURL string `json:"display_url"`
		Image      struct {
			URL string `json:"url"`
		} `json:"image"`
	} `json:"data"`
	Success bool `json:"success"`
	Status  int  `json:"status"`
}

// UploadImage é o proxy server-side para a API do IMGBB.
// Recebe um ficheiro multipart (campo "image") e reenvia ao IMGBB.
// Retorna JSON {"url": "..."} com o URL directo da imagem.
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	// Limitar o tamanho do pedido
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Ficheiro demasiado grande (máx. 10 MB)"}`, http.StatusBadRequest)
		return
	}

	file, fileHeader, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Campo 'image' em falta"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Verificar tamanho
	if fileHeader.Size > maxUploadSize {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Ficheiro demasiado grande (máx. 10 MB)"}`, http.StatusBadRequest)
		return
	}

	// Ler os bytes da imagem
	imgBytes, err := io.ReadAll(file)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Erro ao ler ficheiro"}`, http.StatusInternalServerError)
		return
	}

	// Codificar em base64 para enviar ao IMGBB
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	// Construir o pedido ao IMGBB
	formData := url.Values{}
	formData.Set("key", imgbbAPIKey)
	formData.Set("image", encoded)

	resp, err := http.Post(imgbbUploadURL, "application/x-www-form-urlencoded", strings.NewReader(formData.Encode()))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Erro ao contactar IMGBB"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var imgbbResp imgbbResponse
	if err := json.NewDecoder(resp.Body).Decode(&imgbbResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"Resposta inválida do IMGBB"}`, http.StatusBadGateway)
		return
	}

	if !imgbbResp.Success {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, fmt.Sprintf(`{"error":"IMGBB rejeitou a imagem (status %d)"}`, imgbbResp.Status), http.StatusBadGateway)
		return
	}

	// Preferir display_url (sem expiração) sobre image.url
	imageURL := imgbbResp.Data.DisplayURL
	if imageURL == "" {
		imageURL = imgbbResp.Data.Image.URL
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": imageURL})
}
