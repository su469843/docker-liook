package main

// 作业附件（拍照 / 录音）与老师圈画批注
//
// 设计要点：
//   - 学员可以把作业拍照或录语音上传，文件走统一 Storage（本地/S3/WebDAV），
//     key 固定为 practice/<练习ID>/<学员ID>/<32位hex>.<ext>，便于鉴权与批量清理；
//   - 提交时只带附件 key，服务端按 key 反查归属、校验大小，不信任前端传的路径/大小；
//   - 老师批注用图片像素坐标的图形列表（画笔/圈/方框/箭头/文字），
//     0 依赖前端框架，读取时按原图尺寸缩放即可，导出/换端都不丢。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// PracticeAttachment 学员提交的一个附件
type PracticeAttachment struct {
	Key      string  `json:"key"`
	Kind     string  `json:"kind"` // image | audio
	Name     string  `json:"name,omitempty"`
	Size     int64   `json:"size,omitempty"`
	Duration float64 `json:"duration,omitempty"` // 秒（音频时长）
}

// AnnotationShape 老师的一条批注图形。坐标为图片像素空间（原图宽高）。
type AnnotationShape struct {
	Tool   string    `json:"tool"` // pen | circle | rect | arrow | text
	Color  string    `json:"color"`
	Width  float64   `json:"width"`            // 线宽 / 文字字号
	Points []float64 `json:"points,omitempty"` // pen：x0,y0,x1,y1…
	X      float64   `json:"x,omitempty"`
	Y      float64   `json:"y,omitempty"`
	W      float64   `json:"w,omitempty"`
	H      float64   `json:"h,omitempty"`
	X2     float64   `json:"x2,omitempty"`
	Y2     float64   `json:"y2,omitempty"`
	Text   string    `json:"text,omitempty"`
}

// ---------------- 建表与迁移 ----------------

func (s *Store) ensurePracticeMediaSchema() error {
	// 老库补列；新库在 ensurePracticeSchema 的建表语句里已经带上，重复执行会报错，忽略即可。
	for _, ddl := range []string{
		`ALTER TABLE practices ADD COLUMN allow_photo INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE practices ADD COLUMN allow_voice INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE practice_submissions ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE practice_submissions ADD COLUMN annotations TEXT NOT NULL DEFAULT '{}'`,
	} {
		_, _ = s.db.Exec(ddl)
	}
	return nil
}

// ---------------- 附件 key ----------------

const practiceFileExts = `jpg|jpeg|png|webp|gif|webm|ogg|mp4|m4a|mp3|wav|mpeg|aac`

var practiceFileRe = regexp.MustCompile(`^practice/([0-9]+)/([0-9]+)/([a-f0-9]{32})\.(` + practiceFileExts + `)$`)
var annotationColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// practiceFileKeyParts 解析附件 key，返回练习ID / 上传者ID / 扩展名
func practiceFileKeyParts(key string) (practiceID, uploaderID int64, ext string, ok bool) {
	m := practiceFileRe.FindStringSubmatch(key)
	if m == nil {
		return 0, 0, "", false
	}
	var pid, uid int64
	fmt.Sscanf(m[1], "%d", &pid)
	fmt.Sscanf(m[2], "%d", &uid)
	return pid, uid, "." + strings.ToLower(m[4]), pid > 0 && uid > 0
}

func practiceFileKind(key string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.ToLower(pathExt(key)), ".")) {
	case "jpg", "jpeg", "png", "webp", "gif":
		return "image"
	case "webm", "ogg", "mp4", "m4a", "mp3", "wav", "mpeg", "aac":
		return "audio"
	}
	return ""
}

func pathExt(key string) string {
	i := strings.LastIndexByte(key, '.')
	if i < 0 {
		return ""
	}
	return key[i:]
}

func practiceFileContentType(key string) string {
	switch strings.ToLower(pathExt(key)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".webm":
		return "audio/webm"
	case ".ogg":
		return "audio/ogg"
	case ".m4a", ".mp4", ".aac":
		return "audio/mp4"
	case ".mp3", ".mpeg":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	}
	return "application/octet-stream"
}

func newPracticeFileKey(practiceID, uploaderID int64, ext string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("practice/%d/%d/%s%s", practiceID, uploaderID, hexStr(buf), ext)
}

// ---------------- 附件校验 ----------------

// normalizeAttachments 把前端提交的附件元数据落到服务端可信值上：
// key 必须属于该练习 + 该学员，文件必须真实存在，大小以存储为准。
func (a *API) normalizeAttachments(items []PracticeAttachment, practiceID, uploaderID int64, allowPhoto, allowVoice bool) ([]PracticeAttachment, string) {
	if len(items) > 15 {
		return nil, "附件过多（最多 15 个）"
	}
	seen := make(map[string]bool, len(items))
	out := make([]PracticeAttachment, 0, len(items))
	images, audios := 0, 0
	for _, it := range items {
		pid, uid, _, ok := practiceFileKeyParts(it.Key)
		if !ok || pid != practiceID || uid != uploaderID {
			return nil, "附件无效"
		}
		if seen[it.Key] {
			continue
		}
		seen[it.Key] = true
		kind := practiceFileKind(it.Key)
		if kind == "" {
			return nil, "附件类型不支持"
		}
		if kind == "image" {
			if !allowPhoto {
				return nil, "本次作业不允许上传照片"
			}
			images++
			if images > 9 {
				return nil, "照片最多 9 张"
			}
		} else {
			if !allowVoice {
				return nil, "本次作业不允许语音提交"
			}
			audios++
			if audios > 3 {
				return nil, "语音最多 3 段"
			}
		}
		size, err := a.stg().Stat(context.Background(), it.Key)
		if err != nil || size <= 0 {
			return nil, "附件不存在或已失效"
		}
		name := trimText(it.Name, 120)
		dur := it.Duration
		if math.IsNaN(dur) || math.IsInf(dur, 0) || dur < 0 {
			dur = 0
		}
		if dur > 7200 {
			dur = 7200
		}
		out = append(out, PracticeAttachment{
			Key: it.Key, Kind: kind, Name: name, Size: size,
			Duration: math.Round(dur*10) / 10,
		})
	}
	return out, ""
}

var annotationTools = map[string]bool{"pen": true, "circle": true, "rect": true, "arrow": true, "text": true}

// normalizeAnnotations 清洗老师批注：图形类型/颜色/线宽白名单，坐标夹紧，整体体积限制。
func normalizeAnnotations(raw map[string][]AnnotationShape, allowed map[string]bool) (map[string][]AnnotationShape, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	if len(raw) > 20 {
		return nil, "批注图片过多"
	}
	out := make(map[string][]AnnotationShape, len(raw))
	for key, shapes := range raw {
		if !allowed[key] {
			continue
		}
		if len(shapes) > 400 {
			return nil, "单张图片批注过多"
		}
		clean := make([]AnnotationShape, 0, len(shapes))
		for _, s := range shapes {
			if !annotationTools[s.Tool] {
				continue
			}
			if !annotationColorRe.MatchString(s.Color) {
				s.Color = "#e03131"
			}
			s.Width = clampFloat(s.Width, 1, 24)
			s.Text = trimText(s.Text, 200)
			if s.Tool == "text" && s.Text == "" {
				continue
			}
			if len(s.Points)%2 != 0 {
				s.Points = s.Points[:len(s.Points)-1]
			}
			if len(s.Points) > 4000 {
				s.Points = s.Points[:4000]
			}
			for i := range s.Points {
				s.Points[i] = clampFloat(s.Points[i], 0, 40000)
			}
			s.X = clampFloat(s.X, 0, 40000)
			s.Y = clampFloat(s.Y, 0, 40000)
			s.W = clampFloat(s.W, 0, 40000)
			s.H = clampFloat(s.H, 0, 40000)
			s.X2 = clampFloat(s.X2, 0, 40000)
			s.Y2 = clampFloat(s.Y2, 0, 40000)
			clean = append(clean, s)
		}
		if len(clean) > 0 {
			out[key] = clean
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	if b, err := json.Marshal(out); err != nil || len(b) > 300*1024 {
		return nil, "批注内容过大"
	}
	return out, ""
}

func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------------- 上传 / 读取接口 ----------------

// handleUploadPracticeFile 学员上传作业照片或语音：POST /api/practices/{id}/files
func (a *API) handleUploadPracticeFile(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以上传作业附件")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的练习 ID")
		return
	}
	p, err := a.db.GetPractice(id)
	if err != nil {
		writeErr(w, 404, "练习不存在")
		return
	}
	if !p.Published {
		writeErr(w, http.StatusForbidden, "练习尚未发布")
		return
	}
	enrolled, _ := a.db.IsEnrolled(p.CourseID, u.ID)
	if !enrolled {
		writeErr(w, http.StatusForbidden, "你还没有加入该课程")
		return
	}
	if p.Deadline != "" {
		if t, e := time.Parse(time.RFC3339, p.Deadline); e == nil && time.Now().After(t) {
			writeErr(w, 400, "已过截止时间，不能再上传")
			return
		}
	}
	if existing, e := a.db.GetPracticeSubmission(id, u.ID); e == nil && existing != nil {
		writeErr(w, 400, "你已提交过这份作业，不能重复上传")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	file, head, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "请上传文件（照片 ≤10MB，语音 ≤25MB）")
		return
	}
	defer file.Close()
	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = "image"
	}
	if kind != "image" && kind != "audio" {
		writeErr(w, 400, "附件类型无效")
		return
	}
	headBuf := make([]byte, 512)
	n, _ := io.ReadFull(file, headBuf)
	if n < 8 {
		writeErr(w, 400, "文件无效")
		return
	}
	mime := http.DetectContentType(headBuf[:n])
	// multipart 分片可能带准确的 Content-Type（浏览器录音 Blob 是 audio/webm 等，
	// http.DetectContentType 识别不了 webm/ogg 容器，所以音频优先看分片类型）。
	partType := strings.ToLower(strings.TrimSpace(strings.Split(head.Header.Get("Content-Type"), ";")[0]))
	ext := ""
	switch kind {
	case "image":
		if !p.AllowPhoto {
			writeErr(w, 400, "本次作业不允许上传照片")
			return
		}
		e, ok := imageMimeExt(mime)
		if !ok {
			writeErr(w, 400, "仅支持 png/jpg/gif/webp 图片")
			return
		}
		ext = e
		if head.Size > 10<<20 {
			writeErr(w, 400, "单张照片不能超过 10MB")
			return
		}
	case "audio":
		if !p.AllowVoice {
			writeErr(w, 400, "本次作业不允许语音提交")
			return
		}
		switch {
		case strings.Contains(mime, "webm"), strings.Contains(partType, "webm"):
			ext = ".webm"
		case strings.Contains(mime, "ogg"), strings.Contains(partType, "ogg"), strings.Contains(partType, "opus"):
			ext = ".ogg"
		case strings.Contains(mime, "mp4"), strings.Contains(partType, "mp4"),
			strings.Contains(partType, "m4a"), strings.Contains(partType, "aac"):
			ext = ".m4a"
		case strings.Contains(mime, "mpeg"), strings.Contains(partType, "mpeg"), strings.Contains(partType, "mp3"):
			ext = ".mp3"
		case strings.Contains(mime, "wav"), strings.Contains(partType, "wav"):
			ext = ".wav"
		default:
			writeErr(w, 400, "仅支持 webm/m4a/mp3/ogg/wav 语音")
			return
		}
		if head.Size > 25<<20 {
			writeErr(w, 400, "单段语音不能超过 25MB")
			return
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, "读取失败")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 400, "文件读取失败")
		return
	}
	key := newPracticeFileKey(id, u.ID, ext)
	if err := a.stg().Put(r.Context(), key, data); err != nil {
		writeErr(w, 500, "附件保存失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, PracticeAttachment{
		Key: key, Kind: kind, Name: trimText(head.Filename, 120), Size: int64(len(data)),
	})
}

// handleServePracticeFile 读取作业附件：GET /api/practice-files/{key...}
// 只有上传者本人、或该练习所属课程的管理者（老师/管理员）可以访问。
func (a *API) handleServePracticeFile(w http.ResponseWriter, r *http.Request, u *User) {
	key := r.PathValue("key")
	pid, uid, _, ok := practiceFileKeyParts(key)
	if !ok {
		writeErr(w, 404, "文件不存在")
		return
	}
	allowed := u.ID == uid
	if !allowed {
		p, err := a.db.GetPractice(pid)
		if err == nil {
			if _, ok := a.canManageCourse(u, p.CourseID); ok {
				allowed = true
			}
		}
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "无权查看该附件")
		return
	}
	rc, err := a.stg().Get(r.Context(), key)
	if err != nil {
		writeErr(w, 404, "文件不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", practiceFileContentType(key))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Accept-Ranges", "none")
	io.Copy(w, rc)
}
