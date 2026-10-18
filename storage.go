package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage 是存储后端抽象：本地目录 / S3(MinIO 兼容) / WebDAV
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

func NewStorage(cfg *Config) (Storage, error) {
	switch cfg.StorageType {
	case "s3":
		return newS3(cfg)
	case "webdav":
		return newWebDAV(cfg), nil
	default:
		return newLocal(cfg.DataDir)
	}
}

func contentTypeOf(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".m4s":
		return "video/iso.segment"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// ---------------- 本地目录 ----------------

type localStore struct{ root string }

func newLocal(root string) (*localStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &localStore{root: abs}, nil
}

func (s *localStore) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}

func (s *localStore) Put(_ context.Context, key string, data []byte) error {
	p := s.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (s *localStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(s.path(key))
}

func (s *localStore) Delete(_ context.Context, key string) error {
	os.Remove(s.path(key))
	return nil
}

func (s *localStore) DeletePrefix(_ context.Context, prefix string) error {
	return os.RemoveAll(s.path(prefix))
}

// ---------------- S3 (MinIO 兼容) ----------------

type s3Store struct {
	cli    *minio.Client
	bucket string
}

func newS3(cfg *Config) (*s3Store, error) {
	cli, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure: cfg.S3UseSSL,
	})
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	exists, err := cli.BucketExists(ctx, cfg.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("连接 S3 失败: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.S3Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("创建 bucket 失败: %w", err)
		}
	}
	return &s3Store{cli: cli, bucket: cfg.S3Bucket}, nil
}

func (s *s3Store) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.cli.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentTypeOf(key)})
	return err
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if _, err := s.cli.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err != nil {
		return nil, err
	}
	return s.cli.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	return s.cli.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *s3Store) DeletePrefix(ctx context.Context, prefix string) error {
	items := s.cli.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	for it := range items {
		if it.Err != nil {
			return it.Err
		}
		if err := s.cli.RemoveObject(ctx, s.bucket, it.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- WebDAV ----------------

type webdavStore struct {
	base string
	user string
	pass string
	cli  *http.Client
}

func newWebDAV(cfg *Config) *webdavStore {
	return &webdavStore{
		base: strings.TrimRight(cfg.WebDAVURL, "/"),
		user: cfg.WebDAVUser,
		pass: cfg.WebDAVPass,
		cli:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (s *webdavStore) url(key string) string { return s.base + "/" + key }

func (s *webdavStore) do(method, rawurl string, body []byte, depth string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawurl, rd)
	if err != nil {
		return nil, err
	}
	if s.user != "" || s.pass != "" {
		req.SetBasicAuth(s.user, s.pass)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	return s.cli.Do(req)
}

func (s *webdavStore) mkcolParents(key string) {
	dir := path.Dir(key)
	if dir == "." || dir == "/" {
		return
	}
	cur := ""
	for _, p := range strings.Split(strings.Trim(dir, "/"), "/") {
		cur += "/" + p
		if resp, err := s.do("MKCOL", s.base+cur, nil, ""); err == nil {
			resp.Body.Close()
		}
	}
}

func (s *webdavStore) Put(ctx context.Context, key string, data []byte) error {
	s.mkcolParents(key)
	resp, err := s.do("PUT", s.url(key), data, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("WebDAV PUT %s: %s", key, resp.Status)
	}
	return nil
}

func (s *webdavStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.do("GET", s.url(key), nil, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("WebDAV GET %s: %s", key, resp.Status)
	}
	return resp.Body, nil
}

func (s *webdavStore) Delete(ctx context.Context, key string) error {
	resp, err := s.do("DELETE", s.url(strings.Trim(key, "/")), nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// DeletePrefix 利用 WebDAV DELETE 的递归语义直接删目录
func (s *webdavStore) DeletePrefix(ctx context.Context, prefix string) error {
	resp, err := s.do("DELETE", s.url(strings.Trim(prefix, "/"))+"/", nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
