package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Category string

const (
	CategoryProducts  Category = "products"
	CategoryLogos     Category = "logos"
	CategoryDocuments Category = "documents"
	CategoryMisc      Category = "misc"
)

type Config struct {
	AccountID     string
	AccessKey     string
	SecretKey     string
	Bucket        string
	PublicBaseURL string
	Endpoint      string
}

type R2Store struct {
	Client    *minio.Client
	bucket    string
	publicURL string
}

func NewR2Store(cfg Config) (*R2Store, error) {
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage: R2 access key and secret key are required")
	}
	if cfg.Bucket == "" {
		cfg.Bucket = "nova-media"
	}
	if cfg.Endpoint == "" {
		if cfg.AccountID == "" {
			return nil, errors.New("storage: either Endpoint or AccountID is required")
		}
		cfg.Endpoint = fmt.Sprintf("https://%s.eu.r2.cloudflarestorage.com", cfg.AccountID)
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: true,
		Region: "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create R2 client: %w", err)
	}

	return &R2Store{
		Client:    client,
		bucket:    cfg.Bucket,
		publicURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
	}, nil
}

type UploadResult struct {
	Key         string
	URL         string
	Size        int64
	ContentType string
}

func (s *R2Store) Upload(ctx context.Context, shopID uuid.UUID, category Category, filename string, contentType string, reader io.Reader, size int64) (*UploadResult, error) {
	if err := validateContentType(contentType); err != nil {
		return nil, err
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = extFromContentType(contentType)
	}
	key := fmt.Sprintf("shops/%s/%s/%s%s", shopID.String(), category, uuid.New().String(), ext)

	_, err := s.Client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
		UserMetadata: map[string]string{
			"original-filename": filename,
			"category":          string(category),
			"shop-id":           shopID.String(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: upload to R2: %w", err)
	}

	url := ""
	if s.publicURL != "" {
		url = s.publicURL + "/" + key
	}
	return &UploadResult{Key: key, URL: url, Size: size, ContentType: contentType}, nil
}

func (s *R2Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	return s.Client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *R2Store) PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if expiry == 0 {
		expiry = time.Hour
	}
	url, err := s.Client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("storage: presign URL: %w", err)
	}
	return url.String(), nil
}

func (s *R2Store) BucketName() string { return s.bucket }

var allowedContentTypes = map[string]bool{
	"image/jpeg": true, "image/jpg": true, "image/png": true,
	"image/webp": true, "image/gif": true, "image/svg+xml": true,
	"video/mp4": true, "video/webm": true, "video/quicktime": true,
	"application/pdf": true, "text/plain": true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
}

func validateContentType(ct string) error {
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.TrimSpace(strings.ToLower(ct))
	if !allowedContentTypes[ct] {
		return fmt.Errorf("storage: content type %q is not allowed", ct)
	}
	return nil
}

func extFromContentType(ct string) string {
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.TrimSpace(ct)
	exts, _ := mime.ExtensionsByType(ct)
	if len(exts) > 0 {
		return exts[0]
	}
	return ""
}

const (
	MaxImageSize    = 10 * 1024 * 1024
	MaxVideoSize    = 50 * 1024 * 1024
	MaxDocumentSize = 10 * 1024 * 1024
)

func MaxSizeForContentType(ct string) int64 {
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.TrimSpace(strings.ToLower(ct))
	switch {
	case strings.HasPrefix(ct, "image/"):
		return MaxImageSize
	case strings.HasPrefix(ct, "video/"):
		return MaxVideoSize
	default:
		return MaxDocumentSize
	}
}
