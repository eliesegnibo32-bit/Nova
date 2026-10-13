package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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
	accessKey  string
	secretKey  string
	bucket     string
	endpoint   string
	publicURL  string
	httpClient *http.Client
}

func NewR2Store(cfg Config) (*R2Store, error) {
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("R2 keys required")
	}
	if cfg.Bucket == "" {
		cfg.Bucket = "nova-media"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fmt.Sprintf("https://%s.eu.r2.cloudflarestorage.com", cfg.AccountID)
	}
	return &R2Store{
		accessKey:  cfg.AccessKey,
		secretKey:  cfg.SecretKey,
		bucket:     cfg.Bucket,
		endpoint:   strings.TrimRight(cfg.Endpoint, "/"),
		publicURL:  strings.TrimRight(cfg.PublicBaseURL, "/"),
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type UploadResult struct {
	Key         string
	URL         string
	Size        int64
	ContentType string
}

func (s *R2Store) Upload(ctx context.Context, shopID uuid.UUID, category Category, filename string, contentType string, reader io.Reader, size int64) (*UploadResult, error) {
	ext := filepath.Ext(filename)
	if ext == "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	key := fmt.Sprintf("shops/%s/%s/%s%s", shopID.String(), category, uuid.New().String(), ext)

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)

	// Sign with the ACTUAL body hash (not empty)
	if err := s.signV4(req, key, body); err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("R2 returned %d: %s", resp.StatusCode, string(respBody))
	}

	result := &UploadResult{Key: key, Size: int64(len(body)), ContentType: contentType}
	if s.publicURL != "" {
		result.URL = s.publicURL + "/" + key
	}
	return result, nil
}

func (s *R2Store) Delete(ctx context.Context, key string) error {
	url := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	s.signV4(req, key, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (s *R2Store) BucketName() string { return s.bucket }

func (s *R2Store) signV4(req *http.Request, key string, body []byte) error {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	// Compute the ACTUAL payload hash from the body being sent
	payloadHash := sha256Hex(body)

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// Host comes from the URL — Go sets it automatically, we just read it
	host := req.URL.Host

	// Build canonical headers (sorted alphabetically)
	headers := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	var sortedKeys []string
	for k := range headers {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	canonicalHeaders := ""
	signedHeaders := ""
	for i, k := range sortedKeys {
		canonicalHeaders += k + ":" + strings.TrimSpace(headers[k]) + "\n"
		if i > 0 {
			signedHeaders += ";"
		}
		signedHeaders += k
	}

	// Canonical request
	canonicalRequest := req.Method + "\n" +
		"/" + s.bucket + "/" + key + "\n" +
		"\n" + // empty query string
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	// String to sign
	credentialScope := dateStamp + "/auto/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		sha256Hex([]byte(canonicalRequest))

	// Signing key
	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, "auto")
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	// Authorization header
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, credentialScope, signedHeaders, signature,
	))

	return nil
}

func sha256Hex(d []byte) string { h := sha256.Sum256(d); return hex.EncodeToString(h[:]) }
func hmacSHA256(k []byte, d string) []byte {
	h := hmac.New(sha256.New, k)
	h.Write([]byte(d))
	return h.Sum(nil)
}
