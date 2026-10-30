package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	r.Post("/upload", h.handleUpload)
	r.Post("/upload/product", h.handleUpload)
	r.Post("/upload/logo", h.handleUpload)
	r.Delete("/upload", h.handleDelete)
}

func (h *UploadHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeErrorWithCode(w, http.StatusServiceUnavailable, "storage_unavailable", "Stockage non configuré")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
	if err := r.ParseMultipartForm(10 * 1024 * 1024); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "parse_error", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "missing_file", "Champ file requis")
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	shopID := middleware.ActiveShopIDFromContext(r.Context())
	if shopID == uuid.Nil {
		writeErrorWithCode(w, http.StatusForbidden, "no_shop", "Aucune boutique active")
		return
	}

	cat := storage.CategoryProducts
	result, err := h.store.Upload(r.Context(), shopID, cat, header.Filename, ct, file, header.Size)
	if err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "upload_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "key": result.Key, "url": result.URL, "size": result.Size, "content_type": result.ContentType,
	})
}

func (h *UploadHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	shopID := middleware.ActiveShopIDFromContext(r.Context())
	if shopID == uuid.Nil {
		writeErrorWithCode(w, http.StatusForbidden, "no_shop", "Aucune boutique active")
		return
	}
	if !strings.HasPrefix(req.Key, fmt.Sprintf("shops/%s/", shopID.String())) {
		writeErrorWithCode(w, http.StatusForbidden, "not_owned", "Fichier non autorisé")
		return
	}
	if err := h.store.Delete(r.Context(), req.Key); err != nil {
		writeErrorWithCode(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
