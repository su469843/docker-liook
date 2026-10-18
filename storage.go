package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
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
// 大文件（视频）走 PutStream，播放/下载走 GetRange + Stat，全部支持断点续传与 Range 播放。
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	PutStream(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// GetRange 读取 [off, off+length)；length <= 0 表示读到结尾
	GetRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (int64, error)
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

// configFromSettings 用后台设置覆盖环境变量里的存储配置
func configFromSettings(base *Config, st *Settings) *Config {
	cfg := *base
	if st.StorageType == "" {
		return &cfg
	}
	cfg.StorageType = st.StorageType
	cfg.S3Endpoint = st.S3Endpoint
	cfg.S3Bucket = st.S3Bucket
	cfg.S3AccessKey = st.S3AccessKey
	cfg.S3SecretKey = st.S3SecretKey
	cfg.S3UseSSL = st.S3UseSSL
	cfg.WebDAVURL = st.WebDAVURL
	cfg.WebDAVUser = st.WebDAVUser
	cfg.WebDAVPass = st.WebDAVPass
	return &cfg
}

func storageSig(st *Settings) string {
	if st.StorageType == "" {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%t|%s|%s|%s", st.StorageType, st.S3Endpoint, st.S3Bucket,
		st.S3AccessKey, st.S3SecretKey, st.S3UseSSL, st.WebDAVURL, st.WebDAVUser, st.WebDAVPass)
}

// stg 返回当前生效的存储后端：后台设置优先，改了设置自动重建
func (a *API) stg() Storage {
	st := a.db.GetSettings()
	sig := storageSig(st)
	if sig == "" {
		return a.st
	}
	a.stMu.Lock()
	defer a.stMu.Unlock()
	if sig == a.stSig && a.stCustom != nil {
		return a.stCustom
	}
	ns, err := NewStorage(configFromSettings(a.cfg, st))
	if err != nil {
		log.Printf("存储后端切换失败（沿用原后端）: %v", err)
		return a.st
	}
	a.stCustom, a.stSig = ns, sig
	log.Printf("已切换存储后端：%s", st.StorageType)
	return ns
}

// testStorage 探测某个存储配置是否可写可读（后台「测试连接」用）
func testStorage(cfg *Config) error {
	st, err := NewStorage(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	key := fmt.Sprintf("probe/%d.txt", time.Now().UnixNano())
	if err := st.Put(ctx, key, []byte("ok")); err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}
	defer st.Delete(ctx, key)
	n, err := st.Stat(ctx, key)
	if err != nil {
		return fmt.Errorf("读取失败: %w", err)
	}
	if n != 2 {
		return fmt.Errorf("文件大小异常: %d", n)
	}
	rc, err := st.GetRange(ctx, key, 1, 1)
	if err != nil {
		return fmt.Errorf("Range 读取失败: %w", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "k" {
		return fmt.Errorf("Range 读取内容不一致")
	}
	return nil
}

func contentTypeOf(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".m4s":
		return "video/iso.segment"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".key":
		return "application/octet-stream"
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

type limitedReadCloser struct {
	io.Reader
	io.Closer
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

func (s *localStore) PutStream(_ context.Context, key string, r io.Reader, size int64) error {
	p := s.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && size > 0 && n != size {
		err = fmt.Errorf("写入长度不符：期望 %d 实际 %d", size, n)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (s *localStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(s.path(key))
}

func (s *localStore) GetRange(_ context.Context, key string, off, length int64) (io.ReadCloser, error) {
	f, err := os.Open(s.path(key))
	if err != nil {
		return nil, err
	}
	if off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	if length > 0 {
		return limitedReadCloser{io.LimitReader(f, length), f}, nil
	}
	return f, nil
}

func (s *localStore) Stat(_ context.Context, key string) (int64, error) {
	fi, err := os.Stat(s.path(key))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
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

// PutStream 大文件分片上传（minio-go 自动分片，16MB 一片）
func (s *s3Store) PutStream(ctx context.Context, key string, r io.Reader, size int64) error {
	if size <= 0 {
		return fmt.Errorf("S3 上传需要已知文件大小")
	}
	part := uint64(16 << 20)
	for part < 64<<20 && size/int64(part) > 10000 {
		part *= 2
	}
	_, err := s.cli.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentTypeOf(key),
		PartSize:    part,
	})
	return err
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.cli.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *s3Store) GetRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	end := int64(0) // 0 = 到文件结尾
	if length > 0 {
		end = off + length - 1
	}
	if off > 0 || end > 0 {
		if err := opts.SetRange(off, end); err != nil {
			return nil, err
		}
	}
	return s.cli.GetObject(ctx, s.bucket, key, opts)
}

func (s *s3Store) Stat(ctx context.Context, key string) (int64, error) {
	oi, err := s.cli.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, err
	}
	return oi.Size, nil
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
		cli:  &http.Client{Timeout: 30 * time.Minute},
	}
}

func (s *webdavStore) url(key string) string { return s.base + "/" + key }

func (s *webdavStore) doReq(method, rawurl string, body io.Reader, size int64, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, rawurl, body)
	if err != nil {
		return nil, err
	}
	if s.user != "" || s.pass != "" {
		req.SetBasicAuth(s.user, s.pass)
	}
	if body != nil && size > 0 {
		req.ContentLength = size
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return s.cli.Do(req)
}

func (s *webdavStore) do(method, rawurl string, body []byte, depth string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	hdr := map[string]string{}
	if depth != "" {
		hdr["Depth"] = depth
	}
	return s.doReq(method, rawurl, rd, int64(len(body)), hdr)
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

func (s *webdavStore) PutStream(ctx context.Context, key string, r io.Reader, size int64) error {
	s.mkcolParents(key)
	resp, err := s.doReq("PUT", s.url(key), r, size, nil)
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

func (s *webdavStore) GetRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) {
	hdr := map[string]string{}
	if length > 0 {
		hdr["Range"] = fmt.Sprintf("bytes=%d-%d", off, off+length-1)
	} else if off > 0 {
		hdr["Range"] = fmt.Sprintf("bytes=%d-", off)
	}
	resp, err := s.doReq("GET", s.url(key), nil, 0, hdr)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("WebDAV GET %s: %s", key, resp.Status)
	}
	return resp.Body, nil
}

func (s *webdavStore) Stat(ctx context.Context, key string) (int64, error) {
	resp, err := s.do("HEAD", s.url(key), nil, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("WebDAV HEAD %s: %s", key, resp.Status)
	}
	return resp.ContentLength, nil
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
