package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ================= 媒体：上传 → 服务端转码 HLS(AES-128) → 签名播放 =================
//
// 流程：老师上传视频源文件（默认一次直传，可选分片断点续传）→ 存到存储后端 src/{id}/ →
// 服务端 ffmpeg 转成 HLS 并按 AES-128 加密（key 与 m3u8 一起进存储）→ 课时 content 指向 index.m3u8，
// 前端拿到的播放地址是带有效期的签名 URL（key/分片也会被签到 m3u8 里），过期即无法继续观看。
// 没装 ffmpeg 或转码失败时退化为「源文件 + HTTP Range」渐进播放，功能不会挂掉。

// ---------------- 签名 URL ----------------

func (a *API) signPath(p string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(a.cfg.Secret))
	fmt.Fprintf(mac, "media|%s|%d", p, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *API) signedURL(p string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	return fmt.Sprintf("%s?e=%d&s=%s", p, exp, a.signPath(p, exp))
}

func (a *API) signedWithExp(p string, exp int64) string {
	return fmt.Sprintf("%s?e=%d&s=%s", p, exp, a.signPath(p, exp))
}

func (a *API) checkSign(r *http.Request, p string) bool {
	q := r.URL.Query()
	exp, err := strconv.ParseInt(q.Get("e"), 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	got := q.Get("s")
	want := a.signPath(p, exp)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (a *API) signTTL() time.Duration {
	min := a.cfg.SignTTLMin
	if min <= 0 {
		min = 120
	}
	return time.Duration(min) * time.Minute
}

// 兼容老数据：没有签名时回退 Cookie 鉴权
func (a *API) userFromCookie(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	uid, ok := parseSessionToken(a.cfg.Secret, c.Value)
	if !ok {
		return nil
	}
	u, err := a.db.GetUser(uid)
	if err != nil || u.Status != "active" {
		return nil
	}
	return u
}

// ---------------- 课时装饰：给前端可直接用的播放地址 ----------------

func (a *API) decorateLesson(l *Lesson) {
	if l == nil || l.Kind != "video" {
		return
	}
	sid := strconv.FormatInt(l.ID, 10)
	switch {
	case strings.HasSuffix(l.Content, ".m3u8"):
		l.VideoMode = "hls"
		l.VideoURL = a.signedURL("/hls/"+sid+"/index.m3u8", a.signTTL())
	case l.Content != "":
		l.VideoMode = "mp4"
		l.VideoURL = a.signedURL("/api/lessons/"+sid+"/stream", a.signTTL())
	case l.Status == "ready":
		// 老数据：浏览器切片上传的 hls/{id}/index.m3u8
		l.VideoMode = "hls"
		l.VideoURL = a.signedURL("/hls/"+sid+"/index.m3u8", a.signTTL())
	}
	if l.Kind == "doc" && l.Content != "" {
		l.VideoURL = a.signedURL("/api/lessons/"+sid+"/file", a.signTTL())
	}
}

func (a *API) writeLesson(w http.ResponseWriter, l *Lesson) {
	a.decorateLesson(l)
	writeJSON(w, http.StatusOK, l)
}

func (a *API) writeLessons(w http.ResponseWriter, ls []Lesson) {
	for i := range ls {
		a.decorateLesson(&ls[i])
	}
	if ls == nil {
		ls = []Lesson{}
	}
	writeJSON(w, http.StatusOK, ls)
}

// ---------------- 上传会话（分片 / 断点续传） ----------------

var uploadIDRe = regexp.MustCompile(`^[a-f0-9]{16,32}$`)

type uploadMeta struct {
	ID        string `json:"id"`
	LessonID  int64  `json:"lesson_id"`
	UserID    int64  `json:"user_id"`
	FileName  string `json:"file_name"`
	Size      int64  `json:"size"`
	Chunks    int    `json:"chunks"`
	ChunkSize int64  `json:"chunk_size"`
	CreatedAt int64  `json:"created_at"`
}

var videoExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true,
	".mkv": true, ".avi": true, ".flv": true, ".ts": true, ".wmv": true,
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *API) uploadRoot() string { return filepath.Join(a.cfg.DataDir, "tmp-upload") }

func (a *API) sessionDir(id string) string { return filepath.Join(a.uploadRoot(), id) }

func (a *API) loadUploadMeta(id string) (*uploadMeta, error) {
	if !uploadIDRe.MatchString(id) {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(a.sessionDir(id), "meta.json"))
	if err != nil {
		return nil, err
	}
	var m uploadMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (a *API) sourceKey(lessonID int64, fileName string) string {
	ext := strings.ToLower(path.Ext(fileName))
	if !videoExts[ext] {
		ext = ".mp4"
	}
	return "src/" + strconv.FormatInt(lessonID, 10) + "/source" + ext
}

func (a *API) maxUploadBytes() int64 {
	mb := a.cfg.MaxUploadMB
	if mb <= 0 {
		mb = 4096
	}
	return int64(mb) << 20
}

// checkVideoLesson 校验课时存在、是视频、且当前用户可管理
func (a *API) checkVideoLesson(w http.ResponseWriter, r *http.Request, u *User, needManage bool) (*Lesson, bool) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return nil, false
	}
	l, err := a.db.GetLesson(id)
	if err != nil || l.Kind != "video" {
		writeErr(w, 404, "课时不存在")
		return nil, false
	}
	if needManage {
		if _, ok := a.canManageCourse(u, l.CourseID); !ok {
			writeErr(w, http.StatusForbidden, "无权上传")
			return nil, false
		}
	} else if _, ok := a.canAccessCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权观看该课时")
		return nil, false
	}
	return l, true
}

// handleVideoUploadDirect 一次直传（默认方式）：请求体就是文件字节流
func (a *API) handleVideoUploadDirect(w http.ResponseWriter, r *http.Request, u *User) {
	l, ok := a.checkVideoLesson(w, r, u, true)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = r.Header.Get("X-File-Name")
	}
	if name == "" {
		writeErr(w, 400, "缺少文件名")
		return
	}
	name = path.Base(strings.TrimSpace(name))
	maxBytes := a.maxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := os.MkdirAll(a.uploadRoot(), 0o700); err != nil {
		writeErr(w, 500, "初始化上传失败")
		return
	}
	tmp, err := os.CreateTemp(a.uploadRoot(), "direct-*")
	if err != nil {
		writeErr(w, 500, "初始化上传失败")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	size, err := io.Copy(tmp, r.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || size == 0 {
		writeErr(w, 400, "上传失败或文件为空")
		return
	}
	key := a.sourceKey(l.ID, name)
	f, err := os.Open(tmpName)
	if err != nil {
		writeErr(w, 500, "读取临时文件失败")
		return
	}
	err = a.stg().PutStream(r.Context(), key, f, size)
	f.Close()
	if err != nil {
		writeErr(w, 500, "保存视频失败: "+err.Error())
		return
	}
	a.startTranscode(l.ID, key, name, size)
	nl, _ := a.db.GetLesson(l.ID)
	a.writeLesson(w, nl)
}

// handleVideoUploadInit 分片上传：初始化会话
func (a *API) handleVideoUploadInit(w http.ResponseWriter, r *http.Request, u *User) {
	l, ok := a.checkVideoLesson(w, r, u, true)
	if !ok {
		return
	}
	var req struct {
		FileName string `json:"file_name"`
		Size     int64  `json:"size"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.Size <= 0 {
		writeErr(w, 400, "文件大小无效")
		return
	}
	if req.Size > a.maxUploadBytes() {
		writeErr(w, 400, fmt.Sprintf("文件超过上限 %d MB", a.cfg.MaxUploadMB))
		return
	}
	const chunkSize int64 = 16 << 20
	chunks := int((req.Size + chunkSize - 1) / chunkSize)
	uid := randHex(12)
	dir := a.sessionDir(uid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, 500, "初始化上传失败")
		return
	}
	m := uploadMeta{
		ID: uid, LessonID: l.ID, UserID: u.ID,
		FileName: path.Base(strings.TrimSpace(req.FileName)),
		Size:     req.Size, Chunks: chunks, ChunkSize: chunkSize,
		CreatedAt: time.Now().Unix(),
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o600); err != nil {
		writeErr(w, 500, "初始化上传失败")
		return
	}
	a.db.SetLessonStatus(l.ID, "uploading")
	writeJSON(w, http.StatusOK, map[string]any{
		"upload_id": uid, "chunk_size": chunkSize, "chunks": chunks, "received": []int{},
	})
}

// handleVideoUploadChunk 上传第 index 片（可重复上传，天然补偿重试）
func (a *API) handleVideoUploadChunk(w http.ResponseWriter, r *http.Request, u *User) {
	uploadID := r.PathValue("uploadID")
	m, err := a.loadUploadMeta(uploadID)
	if err != nil {
		writeErr(w, 404, "上传会话不存在或已过期")
		return
	}
	l, err := a.db.GetLesson(m.LessonID)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权上传")
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || idx < 0 || idx >= m.Chunks {
		writeErr(w, 400, "分片序号无效")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, m.ChunkSize+(1<<20))
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, "分片读取失败或超出大小")
		return
	}
	if int64(len(data)) > m.ChunkSize {
		writeErr(w, 400, "分片过大")
		return
	}
	if int64(len(data)) == 0 {
		writeErr(w, 400, "分片为空")
		return
	}
	part := filepath.Join(a.sessionDir(uploadID), fmt.Sprintf("%d.part", idx))
	tmp := part + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		writeErr(w, 500, "写入分片失败")
		return
	}
	if err := os.Rename(tmp, part); err != nil {
		writeErr(w, 500, "保存分片失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "index": idx})
}

func (a *API) receivedChunks(uploadID string) []int {
	entries, _ := os.ReadDir(a.sessionDir(uploadID))
	out := []int{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".part") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(name, ".part")); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// handleVideoUploadStatus 查询已收到的分片（断点续传）
func (a *API) handleVideoUploadStatus(w http.ResponseWriter, r *http.Request, u *User) {
	uploadID := r.PathValue("uploadID")
	m, err := a.loadUploadMeta(uploadID)
	if err != nil {
		writeErr(w, 404, "上传会话不存在或已过期")
		return
	}
	if m.UserID != u.ID {
		if _, ok := a.canManageCourse(u, m.LessonID); !ok {
			writeErr(w, http.StatusForbidden, "无权访问该上传会话")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"upload_id": uploadID, "chunks": m.Chunks, "chunk_size": m.ChunkSize,
		"size": m.Size, "received": a.receivedChunks(uploadID),
	})
}

// handleVideoUploadDone 合并分片 → 存源文件 → 开始转码
func (a *API) handleVideoUploadDone(w http.ResponseWriter, r *http.Request, u *User) {
	uploadID := r.PathValue("uploadID")
	m, err := a.loadUploadMeta(uploadID)
	if err != nil {
		writeErr(w, 404, "上传会话不存在或已过期")
		return
	}
	l, err := a.db.GetLesson(m.LessonID)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权上传")
		return
	}
	dir := a.sessionDir(uploadID)
	merged := filepath.Join(dir, "source.bin")
	out, err := os.Create(merged)
	if err != nil {
		writeErr(w, 500, "合并分片失败")
		return
	}
	var total int64
	for i := 0; i < m.Chunks; i++ {
		pf, err := os.Open(filepath.Join(dir, fmt.Sprintf("%d.part", i)))
		if err != nil {
			out.Close()
			writeErr(w, 400, fmt.Sprintf("分片 %d 缺失，请续传后重试", i))
			return
		}
		n, cerr := io.Copy(out, pf)
		pf.Close()
		if cerr != nil {
			out.Close()
			writeErr(w, 500, "合并分片失败")
			return
		}
		total += n
	}
	if err := out.Close(); err != nil {
		writeErr(w, 500, "合并分片失败")
		return
	}
	if m.Size > 0 && total != m.Size {
		writeErr(w, 400, fmt.Sprintf("分片总大小不符（期望 %d 实际 %d）", m.Size, total))
		return
	}
	key := a.sourceKey(l.ID, m.FileName)
	f, err := os.Open(merged)
	if err != nil {
		writeErr(w, 500, "读取合并文件失败")
		return
	}
	err = a.stg().PutStream(r.Context(), key, f, total)
	f.Close()
	if err != nil {
		writeErr(w, 500, "保存视频失败: "+err.Error())
		return
	}
	os.RemoveAll(dir)
	a.startTranscode(l.ID, key, m.FileName, total)
	nl, _ := a.db.GetLesson(l.ID)
	a.writeLesson(w, nl)
}

// handleVideoStatus 轮询转码状态
func (a *API) handleVideoStatus(w http.ResponseWriter, r *http.Request, u *User) {
	l, ok := a.checkVideoLesson(w, r, u, false)
	if !ok {
		return
	}
	a.decorateLesson(l)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": l.ID, "status": l.Status, "duration": l.Duration, "size": l.Size,
		"video_mode": l.VideoMode, "video_url": l.VideoURL,
	})
}

// ---------------- 服务端转码 ----------------

var transcodeSem chan struct{}
var transcodeSemOnce sync.Once

func (a *API) transcodeSlots() chan struct{} {
	transcodeSemOnce.Do(func() {
		n := a.cfg.TranscodeMax
		if n <= 0 {
			n = 2
		}
		transcodeSem = make(chan struct{}, n)
	})
	return transcodeSem
}

func (a *API) ffmpegPath() string {
	p := strings.TrimSpace(a.cfg.FFmpegPath)
	if p == "" {
		return ""
	}
	if abs, err := exec.LookPath(p); err == nil {
		return abs
	}
	return ""
}

func (a *API) startTranscode(lessonID int64, srcKey, fileName string, size int64) {
	a.db.SetLessonStatus(lessonID, "processing")
	go func() {
		slots := a.transcodeSlots()
		slots <- struct{}{}
		defer func() { <-slots }()
		err := a.transcodeToHLS(lessonID, srcKey)
		if err == nil {
			return
		}
		log.Printf("转码失败 lesson=%d: %v", lessonID, err)
		// 退化：直接播放源文件（HTTP Range 渐进），至少能看
		ext := strings.ToLower(path.Ext(srcKey))
		if ext == ".mp4" || ext == ".m4v" || ext == ".webm" {
			a.db.SetLessonVideo(lessonID, srcKey, size, 0)
			log.Printf("lesson=%d 已退化为源文件直出播放", lessonID)
			return
		}
		a.db.SetLessonStatus(lessonID, "failed")
	}()
}

func (a *API) transcodeToHLS(lessonID int64, srcKey string) error {
	bin := a.ffmpegPath()
	if bin == "" {
		return fmt.Errorf("未找到 ffmpeg（可设置 FFMPEG_PATH；或用 docker 镜像内置版本）")
	}
	sid := strconv.FormatInt(lessonID, 10)
	base := filepath.Join(a.cfg.DataDir, "tmp-transcode")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "l"+sid+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// 1. 取源文件
	inPath := filepath.Join(dir, "in"+path.Ext(srcKey))
	rc, err := a.stg().Get(context.Background(), srcKey)
	if err != nil {
		return fmt.Errorf("读取源文件失败: %w", err)
	}
	f, err := os.Create(inPath)
	if err != nil {
		rc.Close()
		return err
	}
	_, err = io.Copy(f, rc)
	rc.Close()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("下载源文件失败: %w", err)
	}

	// 2. AES-128 密钥 + keyinfo
	key := make([]byte, 16)
	iv := make([]byte, 16)
	rand.Read(key)
	rand.Read(iv)
	keyPath := filepath.Join(dir, "key.bin")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return err
	}
	infoPath := filepath.Join(dir, "keyinfo.txt")
	info := "key.bin\n" + keyPath + "\n0x" + hex.EncodeToString(iv) + "\n"
	if err := os.WriteFile(infoPath, []byte(info), 0o600); err != nil {
		return err
	}

	// 3. 转 HLS
	outPath := filepath.Join(dir, "index.m3u8")
	args := []string{
		"-y", "-hide_banner", "-nostdin", "-i", inPath,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
		"-hls_time", "10", "-hls_playlist_type", "vod",
		"-hls_key_info_file", infoPath,
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		outPath,
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg 执行失败: %v: %s", err, tailText(stderr.String(), 600))
	}

	// 4. 上传 m3u8 / 分片 / 密钥
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	uploaded := 0
	for _, e := range entries {
		name := e.Name()
		if name != "index.m3u8" && name != "key.bin" && !strings.HasPrefix(name, "seg") {
			continue
		}
		sf, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		fi, _ := sf.Stat()
		err = a.stg().PutStream(context.Background(), "hls/"+sid+"/"+name, sf, fi.Size())
		sf.Close()
		if err != nil {
			return fmt.Errorf("上传 %s 失败: %w", name, err)
		}
		uploaded++
	}
	if uploaded < 2 {
		return fmt.Errorf("转码产物不完整（%d 个文件）", uploaded)
	}
	dur := parseFFmpegDuration(stderr.String())
	size, _ := a.stg().Stat(context.Background(), "hls/"+sid+"/index.m3u8")
	_ = size
	a.db.SetLessonVideo(lessonID, "hls/"+sid+"/index.m3u8", 0, dur)
	log.Printf("转码完成 lesson=%d 时长=%.1fs 文件=%d", lessonID, dur, uploaded)
	return nil
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

var durRe = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)

func parseFFmpegDuration(out string) float64 {
	m := durRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	sec, _ := strconv.ParseFloat(m[3], 64)
	return float64(h*3600+mi*60) + sec
}

// ---------------- 播放：HLS（签名 + 重写 m3u8）/ 源文件 Range ----------------

func (a *API) handleHLS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	file := r.PathValue("file")
	if !nameRe.MatchString(file) {
		writeErr(w, 400, "文件名无效")
		return
	}
	sid := strconv.FormatInt(id, 10)
	reqPath := "/hls/" + sid + "/" + file
	signed := a.checkSign(r, reqPath)
	var viewer *User
	if !signed {
		// 老数据/兼容：Cookie 鉴权 + 课程访问控制
		viewer = a.userFromCookie(r)
		if viewer == nil {
			// 先鉴权再查库，避免通过 404/403 探测课时是否存在
			writeErr(w, http.StatusUnauthorized, "未登录或链接已过期")
			return
		}
	}
	l, err := a.db.GetLesson(id)
	if err != nil || l.Kind != "video" {
		writeErr(w, 404, "课时不存在")
		return
	}
	if !signed {
		if _, ok := a.canAccessCourse(viewer, l.CourseID); !ok {
			writeErr(w, http.StatusForbidden, "无权观看该课时")
			return
		}
	}
	key := "hls/" + sid + "/" + file
	if file == "index.m3u8" {
		body, err := a.stg().Get(r.Context(), key)
		if err != nil {
			writeErr(w, 404, "播放列表不存在")
			return
		}
		defer body.Close()
		data, err := io.ReadAll(io.LimitReader(body, 8<<20))
		if err != nil {
			writeErr(w, 500, "读取播放列表失败")
			return
		}
		exp := time.Now().Add(a.signTTL()).Unix()
		out := a.rewritePlaylist(sid, data, exp)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(out)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	a.serveStorageRange(w, r, key, contentTypeOf(file), "")
}

// rewritePlaylist 给 m3u8 里的分片 / 密钥 URI 追加签名，播放器无需自己拼 token
func (a *API) rewritePlaylist(sid string, data []byte, exp int64) []byte {
	base := "/hls/" + sid + "/"
	lines := strings.Split(string(data), "\n")
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, `URI="`) {
				lines[i] = a.rewriteURIs(trimmed, base, exp)
			}
			continue
		}
		lines[i] = a.signedWithExp(base+path.Base(trimmed), exp)
	}
	return []byte(strings.Join(lines, "\n"))
}

func (a *API) rewriteURIs(line, base string, exp int64) string {
	const marker = `URI="`
	for {
		i := strings.Index(line, marker)
		if i < 0 {
			return line
		}
		start := i + len(marker)
		j := strings.Index(line[start:], `"`)
		if j < 0 {
			return line
		}
		uri := line[start : start+j]
		if strings.Contains(uri, "?") {
			return line // 已经是完整 URL
		}
		newURI := a.signedWithExp(base+path.Base(uri), exp)
		line = line[:start] + newURI + line[start+j:]
	}
}

// handleLessonStream 源文件播放（MP4 渐进下载 + Range）
func (a *API) handleLessonStream(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	sid := strconv.FormatInt(id, 10)
	signed := a.checkSign(r, "/api/lessons/"+sid+"/stream")
	var viewer *User
	if !signed {
		viewer = a.userFromCookie(r)
		if viewer == nil {
			writeErr(w, http.StatusUnauthorized, "链接已过期，请刷新页面")
			return
		}
	}
	l, err := a.db.GetLesson(id)
	if err != nil || l.Kind != "video" || l.Content == "" {
		writeErr(w, 404, "视频不存在")
		return
	}
	if !signed {
		if _, ok := a.canAccessCourse(viewer, l.CourseID); !ok {
			writeErr(w, http.StatusForbidden, "无权观看该课时")
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=600")
	a.serveStorageRange(w, r, l.Content, contentTypeOf(l.Content), "")
}

// serveStorageRange 带 Range 的文件输出（视频渐进播放 / 大文件下载）
func (a *API) serveStorageRange(w http.ResponseWriter, r *http.Request, key, ctype, disp string) {
	size, err := a.stg().Stat(r.Context(), key)
	if err != nil || size <= 0 {
		writeErr(w, 404, "文件不存在")
		return
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", ctype)
	if disp != "" {
		w.Header().Set("Content-Disposition", disp)
	}
	start, length, ok := parseRangeHeader(r.Header.Get("Range"), size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if start < 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		rc, err := a.stg().Get(r.Context(), key)
		if err != nil {
			writeErr(w, 404, "文件不存在")
			return
		}
		defer rc.Close()
		io.Copy(w, rc)
		return
	}
	rc, err := a.stg().GetRange(r.Context(), key, start, length)
	if err != nil {
		writeErr(w, 404, "文件不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	io.Copy(w, rc)
}

// parseRangeHeader 解析 "bytes=start-end"（不支持多段，够用）
// 返回 start<0 表示没有 Range 头。
func parseRangeHeader(h string, size int64) (start, length int64, ok bool) {
	h = strings.TrimSpace(h)
	if h == "" || !strings.HasPrefix(h, "bytes=") {
		return -1, 0, true
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if i := strings.Index(spec, ","); i >= 0 {
		spec = spec[:i]
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	beginStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])
	if beginStr == "" {
		// bytes=-N：最后 N 字节
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, n, true
	}
	begin, err := strconv.ParseInt(beginStr, 10, 64)
	if err != nil || begin < 0 || begin >= size {
		return 0, 0, false
	}
	end := size - 1
	if endStr != "" {
		e, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || e < begin {
			return 0, 0, false
		}
		if e < size-1 {
			end = e
		}
	}
	return begin, end - begin + 1, true
}
