package main

// 严格模式 / 监考：HTTP 接口
//
//	POST /api/practices/{id}/proctor/start          学员开始答题（幂等）
//	POST /api/practices/{id}/proctor/event          学员上报离开/抓拍等事件
//	GET  /api/proctor/snapshot/{eid}                老师或本人查看抓拍图
//	GET  /api/practices/{id}/proctor/overview       老师监考台：全部学员汇总
//	GET  /api/practices/{id}/proctor/events         老师查看某个学员的事件时间线
//	POST /api/practices/{id}/submissions/{sid}/feedback-audio  老师语音评语上传
//	GET  /api/practices/{id}/submissions/{sid}/feedback-audio  学员/老师播放语音评语

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// feedbackAudioKeyRe 老师语音评语的存储 key 格式
var feedbackAudioKeyRe = regexp.MustCompile(`^practice-feedback/([0-9]+)/([0-9]+)/([a-f0-9]{32})\.(webm|ogg|m4a|mp3|wav|aac)$`)

// proctorReq 练习创建/更新时的严格模式配置（指针区分「没传」）
type proctorReq struct {
	Mode              *string `json:"mode"`
	MaxExits          *int    `json:"max_exits"`
	RequireFullscreen *bool   `json:"require_fullscreen"`
	RequireCamera     *bool   `json:"require_camera"`
	RequireScreen     *bool   `json:"require_screen"`
}

// toConfig 把前端提交的严格模式配置与旧值合并
func (p *proctorReq) toConfig(old ProctorConfig) (ProctorConfig, string) {
	cfg := old
	if p.Mode != nil {
		m := strings.ToLower(strings.TrimSpace(*p.Mode))
		if m != "normal" && m != "strict" {
			return cfg, "监考模式无效"
		}
		cfg.Mode = m
	}
	if p.MaxExits != nil {
		if *p.MaxExits < 0 || *p.MaxExits > 20 {
			return cfg, "允许离开次数需在 0-20 之间"
		}
		cfg.MaxExits = *p.MaxExits
	}
	if p.RequireFullscreen != nil {
		cfg.RequireFullscreen = *p.RequireFullscreen
	}
	if p.RequireCamera != nil {
		cfg.RequireCamera = *p.RequireCamera
	}
	if p.RequireScreen != nil {
		cfg.RequireScreen = *p.RequireScreen
	}
	if cfg.Mode == "normal" {
		cfg.MaxExits = 0
		cfg.RequireCamera = false
		cfg.RequireScreen = false
	}
	if !cfg.valid() {
		return cfg, "监考配置无效"
	}
	return cfg, ""
}

// loadPracticeForStudent 取练习并校验「已发布 + 已加入课程」
func (a *API) loadPracticeForStudent(u *User, practiceID int64) (*Practice, string) {
	p, err := a.db.GetPractice(practiceID)
	if err != nil {
		return nil, "练习不存在"
	}
	if !p.Published {
		return nil, "练习尚未发布"
	}
	if ok, _ := a.db.IsEnrolled(p.CourseID, u.ID); !ok {
		return nil, "你还没有加入该课程"
	}
	return p, ""
}

// practiceSubmitted 学员是否已提交
func (a *API) practiceSubmitted(practiceID, studentID int64) bool {
	sub, err := a.db.GetPracticeSubmission(practiceID, studentID)
	return err == nil && sub != nil
}

// handleProctorStart 学员点击「开始答题」：记录开始时间与设备信息（幂等，可续答）
func (a *API) handleProctorStart(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以开始答题")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的练习 ID")
		return
	}
	p, msg := a.loadPracticeForStudent(u, id)
	if msg != "" {
		writeErr(w, http.StatusForbidden, msg)
		return
	}
	if a.practiceSubmitted(id, u.ID) {
		writeErr(w, 400, "你已提交过这份练习")
		return
	}
	cfg := a.db.GetProctorConfig(id)
	if cfg.Mode != "strict" {
		writeErr(w, 400, "该练习未开启严格监考")
		return
	}
	var body struct {
		Detail ProctorDetail `json:"detail"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // 允许空 body
	d := body.Detail
	d.IP = clientIP(r)
	if d.UA == "" {
		d.UA = trimText(r.UserAgent(), 300)
	}
	d.Reason = "start"
	startedAt, err := a.db.ProctorStart(id, u.ID, proctorDetailJSON(d))
	if err != nil {
		writeErr(w, 500, "开始答题失败: "+err.Error())
		return
	}
	attempt := a.db.ProctorAttemptInfo(id, u.ID)
	attempt.StartedAt = startedAt
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"attempt":  attempt,
		"proctor":  cfg,
		"duration": p.Duration,
	})
}

// handleProctorEvent 学员上报监考事件（离开/返回/复制/粘贴/抓拍/媒体授权）
func (a *API) handleProctorEvent(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以上报监考事件")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的练习 ID")
		return
	}
	if _, msg := a.loadPracticeForStudent(u, id); msg != "" {
		writeErr(w, http.StatusForbidden, msg)
		return
	}
	cfg := a.db.GetProctorConfig(id)
	if cfg.Mode != "strict" {
		writeErr(w, 400, "该练习未开启严格监考")
		return
	}
	if a.practiceSubmitted(id, u.ID) {
		writeErr(w, 400, "已交卷，不能再上报事件")
		return
	}
	// 限流：正常答题每分钟几次事件足够，防止构造大量上报
	if !a.rate().allow(fmt.Sprintf("proctor:%d:%d", id, u.ID), 1, 60) {
		writeErr(w, http.StatusTooManyRequests, "事件上报过于频繁")
		return
	}
	var body struct {
		Kind   string        `json:"kind"`
		Detail ProctorDetail `json:"detail"`
		Image  string        `json:"image"` // dataURL，可选
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	if !proctorEventKinds[kind] {
		writeErr(w, 400, "事件类型无效")
		return
	}
	// 必须先有开始记录
	if attempt := a.db.ProctorAttemptInfo(id, u.ID); attempt.StartedAt == "" {
		writeErr(w, 400, "请先开始答题")
		return
	}
	d := body.Detail
	d.IP = clientIP(r)
	if d.UA == "" {
		d.UA = trimText(r.UserAgent(), 300)
	}
	imageKey := ""
	if strings.TrimSpace(body.Image) != "" {
		key, err := a.saveProctorSnapshot(id, u.ID, body.Image)
		if err != nil {
			// 截图失败不阻断事件上报（记录原因，老师端仍能看到离开次数）
			d.Extra = "snapshot_failed:" + err.Error()
		} else {
			imageKey = key
		}
	}
	if _, err := a.db.AddProctorEvent(id, u.ID, kind, proctorDetailJSON(d), imageKey); err != nil {
		writeErr(w, 500, "事件保存失败")
		return
	}
	exits, _ := a.db.ProctorExitCount(id, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"exits":     exits,
		"max_exits": cfg.MaxExits,
		"over":      exits > cfg.MaxExits,
	})
}

// handleServeProctorSnapshot 抓拍图：老师/管理员或该学员本人可看
func (a *API) handleServeProctorSnapshot(w http.ResponseWriter, r *http.Request, u *User) {
	eid, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
	if err != nil || eid <= 0 {
		writeErr(w, 400, "无效的截图 ID")
		return
	}
	ev, err := a.db.ProctorEventByID(eid)
	if err != nil || ev.ImageKey == "" {
		writeErr(w, 404, "截图不存在")
		return
	}
	p, err := a.db.GetPractice(ev.PracticeID)
	if err != nil {
		writeErr(w, 404, "截图不存在")
		return
	}
	allowed := u.ID == ev.StudentID
	if !allowed {
		if _, ok := a.canManageCourse(u, p.CourseID); ok {
			allowed = true
		}
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "无权查看该截图")
		return
	}
	rc, err := a.stg().Get(r.Context(), ev.ImageKey)
	if err != nil {
		writeErr(w, 404, "截图不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", practiceFileContentType(ev.ImageKey))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, rc)
}

// handleProctorOverview 老师监考台：全部学员汇总（含未开始/未提交）
func (a *API) handleProctorOverview(w http.ResponseWriter, r *http.Request, u *User) {
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
	if _, ok := a.canManageCourse(u, p.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权查看该练习的监考记录")
		return
	}
	list, err := a.db.ProctorOverview(id)
	if err != nil {
		writeErr(w, 500, "查询监考记录失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proctor":  a.db.GetProctorConfig(id),
		"students": list,
	})
}

// handleProctorEvents 老师查看某个学员的完整事件时间线
func (a *API) handleProctorEvents(w http.ResponseWriter, r *http.Request, u *User) {
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
	if _, ok := a.canManageCourse(u, p.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权查看该练习的监考记录")
		return
	}
	sid, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("student_id")), 10, 64)
	if err != nil || sid <= 0 {
		writeErr(w, 400, "无效的学员 ID")
		return
	}
	events, err := a.db.ListProctorEvents(id, sid)
	if err != nil {
		writeErr(w, 500, "查询事件失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// ---------------- 老师语音评语 ----------------

var feedbackAudioExts = map[string]string{
	"audio/webm": ".webm", "audio/ogg": ".ogg", "audio/mp4": ".m4a",
	"audio/mpeg": ".mp3", "audio/wav": ".wav", "audio/aac": ".aac",
}

// handleUploadFeedbackAudio 老师给某份提交录一段语音评语（≤3MB）
func (a *API) handleUploadFeedbackAudio(w http.ResponseWriter, r *http.Request, u *User) {
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
	if _, ok := a.canManageCourse(u, p.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权批改该练习")
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil || sid <= 0 {
		writeErr(w, 400, "无效的提交 ID")
		return
	}
	sub, err := a.db.GetSubmissionByID(sid)
	if err != nil || sub.PracticeID != id {
		writeErr(w, 404, "提交不存在")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	file, head, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "请上传语音（≤3MB）")
		return
	}
	defer file.Close()
	headBuf := make([]byte, 512)
	n, _ := io.ReadFull(file, headBuf)
	if n < 8 {
		writeErr(w, 400, "文件无效")
		return
	}
	// 优先信任 multipart 分片自带的 Content-Type（录音 Blob 会带上 audio/webm 等），
	// 因为 http.DetectContentType 识别不了 webm/ogg 这类容器格式。
	ct := strings.ToLower(strings.TrimSpace(strings.Split(head.Header.Get("Content-Type"), ";")[0]))
	ext, ok := feedbackAudioExts[ct]
	if !ok {
		ext, ok = feedbackAudioExts[strings.Split(http.DetectContentType(headBuf[:n]), ";")[0]]
	}
	if !ok {
		writeErr(w, 400, "仅支持 webm/ogg/m4a/mp3/wav 语音")
		return
	}
	if head.Size > 3<<20 {
		writeErr(w, 400, "语音评语不能超过 3MB")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, "读取失败")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 400, "读取失败")
		return
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	key := fmt.Sprintf("practice-feedback/%d/%d/%s%s", id, sid, hexStr(buf), ext)
	if err := a.stg().Put(r.Context(), key, data); err != nil {
		writeErr(w, 500, "保存失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key": key, "size": len(data), "url": fmt.Sprintf("/api/practices/%d/submissions/%d/feedback-audio", id, sid),
	})
}

// handleServeFeedbackAudio 播放老师语音评语：学员本人或课程管理者
func (a *API) handleServeFeedbackAudio(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的练习 ID")
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil || sid <= 0 {
		writeErr(w, 400, "无效的提交 ID")
		return
	}
	p, err := a.db.GetPractice(id)
	if err != nil {
		writeErr(w, 404, "练习不存在")
		return
	}
	sub, err := a.db.GetSubmissionByID(sid)
	if err != nil || sub.PracticeID != id {
		writeErr(w, 404, "提交不存在")
		return
	}
	allowed := u.ID == sub.StudentID
	if !allowed {
		if _, ok := a.canManageCourse(u, p.CourseID); ok {
			allowed = true
		}
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "无权播放该语音评语")
		return
	}
	if sub.FeedbackAudio == "" {
		writeErr(w, 404, "还没有语音评语")
		return
	}
	rc, err := a.stg().Get(r.Context(), sub.FeedbackAudio)
	if err != nil {
		writeErr(w, 404, "语音评语不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", practiceFileContentType(sub.FeedbackAudio))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, rc)
}

// strictSubmitGuard 严格模式下提交前的服务端校验：必须已开始、计算超时与离开次数
func (a *API) strictSubmitGuard(p *Practice, studentID int64) (late bool, exits int, msg string) {
	cfg := a.db.GetProctorConfig(p.ID)
	if cfg.Mode != "strict" {
		return false, 0, ""
	}
	startedAt := a.db.ProctorStartedAt(p.ID, studentID)
	if startedAt == "" {
		return false, 0, "请先点击「开始答题」再交卷"
	}
	exits, _ = a.db.ProctorExitCount(p.ID, studentID)
	if p.Duration > 0 {
		if t, ok := parseRFC3339(startedAt); ok && time.Now().After(t.Add(time.Duration(p.Duration)*time.Minute+2*time.Minute)) {
			late = true
		}
	}
	return late, exits, ""
}
