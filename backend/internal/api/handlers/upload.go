package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"nova-api/internal/api/middleware"
	"nova-api/internal/storage"
)

type UploadHandler struct {
	store *storage.R2Store
}

func NewUploadHandler(store *storage.R2Store) *UploadHandler {
	return &UploadHandler{store: store}
}

func (h *UploadHandler) Register(r chi.Router) {
	r.Post("/upload", h.Upload)
	r.Post("/upload/product", h.UploadProduct)
	r.Post("/upload/logo", h.UploadLogo)
	r.Delete("/upload", h.Delete)
	r.Post("/upload/presign", h.PresignURL)
}

type UploadResponse struct {
	OK          bool   `json:"ok"`
	Key         string `json:"key"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Category    string `json:"category"`
}

func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	h.handleUpload(w, r, "")
}

func (h *UploadHandler) UploadProduct(w http.ResponseWriter, r *http.Request) {
	h.handleUpload(w, r, string(storage.CategoryProducts))
}

func (h *UploadHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	h.handleUpload(w, r, string(storage.CategoryLogos))
}

func (h *UploadHandler) handleUpload(w http.ResponseWriter, r *http.Request, defaultCategory string) {
	if h.store == nil {
		writeErrorWithCode(w, http.StatusServiceUnavailable, "storage_unavailable",
			"Le stockage de fichiers n'est pas configuré.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, storage.MaxVideoSize)
	if err := r.ParseMultipartForm(storage.MaxVideoSize); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "parse_error",
			"Fichier trop volumineux ou format invalide: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "missing_file",
			"Le champ 'file' est requis.")
		return
	}
	defer file.Close()

	category := r.FormValue("category")
	if category == "" {
		category = defaultCategory
	}
	if category == "" {
		category = string(storage.CategoryMisc)
	}
	cat := storage.Category(category)
	switch cat {
	case storage.CategoryProducts, storage.CategoryLogos, storage.CategoryDocuments, storage.CategoryMisc:
	default:
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_category",
			"Catégorie invalide. Valeurs: products, logos, documents, misc.")
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	maxSize := storage.MaxSizeForContentType(contentType)
	if header.Size > maxSize {
		writeErrorWithCode(w, http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("Taille max pour ce type: %d MB (reçu: %d MB)", maxSize/1024/1024, header.Size/1024/1024))
		return
	}

	shopID := middleware.ActiveShopIDFromContext(r.Context())
	if shopID == uuid.Nil {
		writeErrorWithCode(w, http.StatusForbidden, "no_shop_context",
			"Aucune boutique active dans la session.")
		return
	}

	result, err := h.store.Upload(r.Context(), shopID, cat, header.Filename, contentType, file, header.Size)
	if err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "upload_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, UploadResponse{
		OK:          true,
		Key:         result.Key,
		URL:         result.URL,
		Size:        result.Size,
		ContentType: result.ContentType,
		Category:    string(cat),
	})
}

type DeleteRequest struct {
	Key string `json:"key" validate:"required"`
}

func (h *UploadHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeErrorWithCode(w, http.StatusServiceUnavailable, "storage_unavailable",
			"Le stockage n'est pas configuré.")
		return
	}

	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
			"Corps de requête invalide.")
		return
	}
	if req.Key == "" {
		writeErrorWithCode(w, http.StatusBadRequest, "missing_key",
			"Le champ 'key' est requis.")
		return
	}

	shopID := middleware.ActiveShopIDFromContext(r.Context())
	if shopID == uuid.Nil {
		writeErrorWithCode(w, http.StatusForbidden, "no_shop_context",
			"Aucune boutique active.")
		return
	}
	expectedPrefix := fmt.Sprintf("shops/%s/", shopID.String())
	if !strings.HasPrefix(req.Key, expectedPrefix) {
		writeErrorWithCode(w, http.StatusForbidden, "key_not_owned",
			"Ce fichier n'appartient pas à votre boutique.")
		return
	}

	if err := h.store.Delete(r.Context(), req.Key); err != nil {
		writeErrorWithCode(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *UploadHandler) PresignURL(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeErrorWithCode(w, http.StatusServiceUnavailable, "storage_unavailable",
			"Le stockage n'est pas configuré.")
		return
	}

	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
			"Corps de requête invalide.")
		return
	}

	url, err := h.store.PresignURL(r.Context(), req.Key, 0)
	if err != nil {
		writeErrorWithCode(w, http.StatusInternalServerError, "presign_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

func (h *UploadHandler) HealthCheck(ctx context.Context) error {
	if h.store == nil {
		return errors.New("storage not configured")
	}
	objCh := h.store.Client.ListObjects(ctx, h.store.BucketName(), minio.ListObjectsOptions{
		Recursive: false,
		MaxKeys:   1,
	})
	for range objCh {
		return nil
	}
	return nil
}
