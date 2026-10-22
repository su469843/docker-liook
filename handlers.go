package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type API struct {
	cfg     *Config
	st      Storage // 环境变量配置的默认后端
	db      *Store
	hub     *Hub
	limiter *loginLimiter

	// 后台可切换的存储后端（设置里配了 storage_type 时覆盖默认后端）
	stMu     sync.Mutex
	stCustom Storage
	stSig    string

	// 通用限流（注册 / 登录 IP / 聊天 / 信令）
	rl *rateLimiter
}

// rate 惰性初始化限流器（测试构造 API 时可能没显式赋值）
func (a *API) rate() *rateLimiter {
	if a.rl == nil {
		a.rl = newRateLimiter()
	}
	return a.rl
}

func (a *API) routes(mux *http.ServeMux) {
	// 公开
	mux.HandleFunc("POST /api/register", a.handleRegister)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("GET /api/config", a.handleConfig)

	// 登录用户
	mux.HandleFunc("GET /api/me", a.requireUser(a.handleMe))
	mux.HandleFunc("POST /api/logout", a.requireUser(a.handleLogout))

	// 课程：建课只有老师/管理员
	mux.HandleFunc("GET /api/courses", a.requireUser(a.handleListCourses))
	mux.HandleFunc("POST /api/courses", a.requireRole(a.handleCreateCourse, "teacher", "admin"))
	mux.HandleFunc("GET /api/courses/{id}", a.requireUser(a.handleGetCourse))
	mux.HandleFunc("PUT /api/courses/{id}", a.requireUser(a.handleUpdateCourse))
	mux.HandleFunc("DELETE /api/courses/{id}", a.requireUser(a.handleDeleteCourse))
	mux.HandleFunc("POST /api/courses/{id}/publish", a.requireUser(a.handlePublishCourse))
	mux.HandleFunc("POST /api/courses/{id}/invite", a.requireUser(a.handleNewInviteCode))
	mux.HandleFunc("POST /api/courses/{id}/cover", a.requireUser(a.handleUploadCover))
	mux.HandleFunc("GET /api/courses/{id}/cover", a.requireUser(a.handleCourseCover))
	mux.HandleFunc("GET /api/courses/{id}/students", a.requireUser(a.handleCourseStudents))
	mux.HandleFunc("POST /api/courses/{id}/teacher", a.requireRole(a.handleAssignTeacher, "admin"))

	// 邀请码 / 课程市场
	mux.HandleFunc("POST /api/join", a.requireUser(a.handleJoinByCode))
	mux.HandleFunc("GET /api/market", a.requireUser(a.handleMarket))
	mux.HandleFunc("POST /api/market/{id}/enroll", a.requireUser(a.handleMarketEnroll))

	// 课时（视频 / 图文 / 文档）
	mux.HandleFunc("GET /api/courses/{id}/lessons", a.requireUser(a.handleListLessons))
	mux.HandleFunc("POST /api/courses/{id}/lessons", a.requireUser(a.handleCreateLesson))
	mux.HandleFunc("GET /api/lessons/{id}", a.requireUser(a.handleGetLesson))
	mux.HandleFunc("PUT /api/lessons/{id}", a.requireUser(a.handleUpdateLesson))
	mux.HandleFunc("DELETE /api/lessons/{id}", a.requireUser(a.handleDeleteLesson))
	mux.HandleFunc("POST /api/lessons/{id}/complete", a.requireUser(a.handleCompleteLesson))
	// 视频上传：默认一次直传；大文件可走分片断点续传（/upload/*）
	mux.HandleFunc("POST /api/lessons/{id}/video", a.requireUser(a.handleVideoUploadDirect))
	mux.HandleFunc("POST /api/lessons/{id}/upload/init", a.requireUser(a.handleVideoUploadInit))
	mux.HandleFunc("PUT /api/lessons/{id}/upload/{uploadID}/{index}", a.requireUser(a.handleVideoUploadChunk))
	mux.HandleFunc("GET /api/lessons/{id}/upload/{uploadID}", a.requireUser(a.handleVideoUploadStatus))
	mux.HandleFunc("POST /api/lessons/{id}/upload/{uploadID}/done", a.requireUser(a.handleVideoUploadDone))
	mux.HandleFunc("GET /api/lessons/{id}/video/status", a.requireUser(a.handleVideoStatus))
	mux.HandleFunc("POST /api/lessons/{id}/doc", a.requireUser(a.handleUploadDoc))
	mux.HandleFunc("GET /api/lessons/{id}/file", a.requireUser(a.handleLessonFile))
	mux.HandleFunc("GET /api/lessons/{id}/stream", a.handleLessonStream)

	// 媒体（签名 URL 或 Cookie 鉴权 + 课程访问控制 + Range）
	mux.HandleFunc("GET /hls/{id}/{file}", a.handleHLS)
	mux.HandleFunc("GET /files/", a.requireUser(a.handleServeFile))
	mux.HandleFunc("POST /api/upload/image", a.requireUser(a.handleImageUpload))
	mux.HandleFunc("POST /api/chat/upload", a.requireUser(a.handleImageUpload))

	// 聊天 / 群聊 / 在线
	mux.HandleFunc("GET /api/contacts", a.requireUser(a.handleContacts))
	mux.HandleFunc("GET /api/chat/{peer}", a.requireUser(a.handleChatHistory))
	mux.HandleFunc("POST /api/chat/{peer}/read", a.requireUser(a.handleChatRead))
	mux.HandleFunc("GET /api/groups", a.requireUser(a.handleMyGroups))
	mux.HandleFunc("GET /api/groups/{id}/messages", a.requireUser(a.handleGroupHistory))
	mux.HandleFunc("POST /api/groups/{id}/read", a.requireUser(a.handleGroupRead))
	mux.HandleFunc("GET /api/ice", a.requireUser(a.handleIce))
	mux.HandleFunc("GET /ws", a.handleWS)

	// 作业 / 练习 / 打卡
	mux.HandleFunc("GET /api/practices", a.requireUser(a.handleListPractices))
	mux.HandleFunc("POST /api/courses/{id}/practices", a.requireRole(a.handleCreatePractice, "teacher", "admin"))
	mux.HandleFunc("GET /api/practices/{id}", a.requireUser(a.handleGetPractice))
	mux.HandleFunc("PUT /api/practices/{id}", a.requireRole(a.handleUpdatePractice, "teacher", "admin"))
	mux.HandleFunc("DELETE /api/practices/{id}", a.requireRole(a.handleDeletePractice, "teacher", "admin"))
	mux.HandleFunc("POST /api/practices/{id}/submit", a.requireUser(a.handleSubmitPractice))
	mux.HandleFunc("GET /api/practices/{id}/submissions", a.requireUser(a.handleListPracticeSubmissions))
	mux.HandleFunc("POST /api/practices/{id}/submissions/{sid}/grade", a.requireUser(a.handleGradePracticeSubmission))
	mux.HandleFunc("GET /api/checkins", a.requireUser(a.handleListCheckins))
	mux.HandleFunc("POST /api/checkins", a.requireUser(a.handleCreateCheckin))
	mux.HandleFunc("GET /api/courses/{id}/checkins", a.requireUser(a.handleCourseCheckins))

	// 站点设置（管理员）
	mux.HandleFunc("GET /api/settings", a.requireRole(a.handleGetSettings, "admin"))
	mux.HandleFunc("PUT /api/settings", a.requireRole(a.handleUpdateSettings, "admin"))
	mux.HandleFunc("POST /api/settings/storage/test", a.requireRole(a.handleStorageTest, "admin"))
	mux.HandleFunc("POST /api/settings/invite", a.requireRole(a.handleNewOrgInvite, "admin"))

	// 用户管理（管理员）
	mux.HandleFunc("GET /api/users", a.requireRole(a.handleListUsers, "admin"))
	mux.HandleFunc("POST /api/users", a.requireRole(a.handleCreateUser, "admin"))
	mux.HandleFunc("POST /api/users/{id}/status", a.requireRole(a.handleSetUserStatus, "admin"))
	mux.HandleFunc("POST /api/users/{id}/password", a.requireRole(a.handleResetPassword, "admin"))
}

// ---------------- 工具 ----------------

// 白名单：首字符必须是字母/数字，防止 ".."、"." 之类的名字落到存储层
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

const inviteAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func genInviteCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(normalizeJSON(v))
}

// normalizeJSON 把 nil 切片/映射转成空数组/空对象，避免前端拿到 null 报错。
func normalizeJSON(v any) any {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return v
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func hlsKey(id int64, file string) string {
	return "hls/" + strconv.FormatInt(id, 10) + "/" + file
}

func imageMimeExt(mimeType string) (string, bool) {
	switch mimeType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	}
	return "", false
}

func imageContentType(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "application/octet-stream"
}

func trimTitle(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

// canAccessCourse 课程的可见性：管理员全部；老师限自己的课；学员限已加入的课
func (a *API) canAccessCourse(u *User, courseID int64) (*Course, bool) {
	c, err := a.db.GetCourse(courseID)
	if err != nil {
		return nil, false
	}
	switch u.Role {
	case "admin":
		return c, true
	case "teacher":
		return c, c.TeacherID == u.ID
	default:
		ok, _ := a.db.IsEnrolled(courseID, u.ID)
		return c, ok
	}
}

// canManageCourse 课程的管理权（上传/删除/看学生）：管理员或任课老师
func (a *API) canManageCourse(u *User, courseID int64) (*Course, bool) {
	c, err := a.db.GetCourse(courseID)
	if err != nil {
		return nil, false
	}
	if u.Role == "admin" || (u.Role == "teacher" && c.TeacherID == u.ID) {
		return c, true
	}
	return nil, false
}

func (a *API) canViewLessons(u *User, c *Course) bool {
	if a.canManage(c, u) {
		return true
	}
	ok, _ := a.db.IsEnrolled(c.ID, u.ID)
	return ok
}

func (a *API) canManage(c *Course, u *User) bool {
	return u.Role == "admin" || (u.Role == "teacher" && c.TeacherID == u.ID)
}

// ---------------- 课程 ----------------

func (a *API) handleListCourses(w http.ResponseWriter, r *http.Request, u *User) {
	var courses []Course
	var err error
	switch u.Role {
	case "admin":
		courses, err = a.db.ListCoursesAll()
	case "teacher":
		courses, err = a.db.ListCoursesByTeacher(u.ID)
	default:
		courses, err = a.db.ListCoursesForStudent(u.ID)
	}
	if err != nil {
		writeErr(w, 500, "查询课程失败: "+err.Error())
		return
	}
	if courses == nil {
		courses = []Course{}
	}
	for i := range courses {
		if u.Role == "student" {
			courses[i].InviteCode = ""
		} else if a.canManage(&courses[i], u) {
			courses[i].CanManage = true
		}
	}
	writeJSON(w, http.StatusOK, courses)
}

// 默认封面配色白名单：前端按「渐变底色 + 课程标题 + 手绘图标」现场渲染封面图，
// 老师建课时选一个颜色即可，不必再找图。
var coverColors = map[string]bool{
	"green": true, "blue": true, "indigo": true, "orange": true,
	"pink": true, "teal": true, "slate": true, "gold": true,
}

func validCoverColor(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "", true
	}
	return v, coverColors[v]
}

// 封面底色（按课程 ID 派生，未选颜色时用）
var coverColorOrder = []string{"green", "blue", "orange", "indigo", "teal", "pink", "slate", "gold"}

func defaultCoverColor(id int64) string {
	n := id % int64(len(coverColorOrder))
	if n < 0 {
		n += int64(len(coverColorOrder))
	}
	return coverColorOrder[n]
}

func (a *API) handleCreateCourse(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Category    string `json:"category"`
		CoverColor  string `json:"cover_color"`
		TeacherID   int64  `json:"teacher_id"` // 仅管理员创建时指定任课老师
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeErr(w, 400, "课程名称不能为空")
		return
	}
	color, ok := validCoverColor(req.CoverColor)
	if !ok {
		writeErr(w, 400, "封面颜色无效")
		return
	}
	teacherID := u.ID
	if u.Role == "admin" && req.TeacherID > 0 {
		t, err := a.db.GetUser(req.TeacherID)
		if err != nil || t.Role != "teacher" {
			writeErr(w, 400, "指定的任课老师不存在或不是老师")
			return
		}
		teacherID = req.TeacherID
	}
	id, err := a.db.CreateCourse(teacherID, trimTitle(req.Title),
		strings.TrimSpace(req.Description), trimTitle(req.Category), color, genInviteCode())
	if err != nil {
		writeErr(w, 500, "创建课程失败: "+err.Error())
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil {
		writeErr(w, 500, "课程回查失败: "+err.Error())
		return
	}
	c.CanManage = true
	writeJSON(w, http.StatusOK, c)
}

func (a *API) handleGetCourse(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil {
		writeErr(w, 404, "课程不存在")
		return
	}
	c.Enrolled, _ = a.db.IsEnrolled(c.ID, u.ID)
	c.CanManage = a.canManage(c, u)
	if !c.CanManage && !c.Enrolled && !c.Published {
		writeErr(w, http.StatusForbidden, "课程不存在或未开放")
		return
	}
	if !c.CanManage {
		c.InviteCode = ""
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) handleUpdateCourse(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, ok := a.canManageCourse(u, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "无权修改该课程")
		return
	}
	var req struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Category    string  `json:"category"`
		CoverColor  *string `json:"cover_color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = c.Title
	}
	if len([]rune(title)) > 80 {
		writeErr(w, 400, "课程名称过长")
		return
	}
	color := c.CoverColor
	if req.CoverColor != nil {
		nc, ok := validCoverColor(*req.CoverColor)
		if !ok {
			writeErr(w, 400, "封面颜色无效")
			return
		}
		color = nc
	}
	if err := a.db.UpdateCourse(id, title, strings.TrimSpace(req.Description), trimTitle(req.Category), color); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	nc, _ := a.db.GetCourse(id)
	nc.CanManage = true
	writeJSON(w, http.StatusOK, nc)
}

func (a *API) handlePublishCourse(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权操作该课程")
		return
	}
	var req struct {
		Published bool `json:"published"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if err := a.db.SetCoursePublished(id, req.Published); err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"published": req.Published})
}

func (a *API) handleDeleteCourse(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, ok := a.canManageCourse(u, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "无权删除该课程")
		return
	}
	ids, err := a.db.LessonIDsOfCourse(id)
	if err != nil {
		writeErr(w, 500, "查询课时失败: "+err.Error())
		return
	}
	for _, lid := range ids {
		sid := strconv.FormatInt(lid, 10)
		a.stg().DeletePrefix(r.Context(), "hls/"+sid)
		a.stg().DeletePrefix(r.Context(), "doc/"+sid)
	}
	if c.Cover != "" {
		a.stg().Delete(r.Context(), c.Cover)
	}
	if err := a.db.DeleteCourse(id); err != nil {
		writeErr(w, 500, "删除课程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleNewInviteCode(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权操作该课程")
		return
	}
	code := genInviteCode()
	if err := a.db.SetInviteCode(id, code); err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"invite_code": code})
}

func (a *API) handleCourseStudents(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权查看该课程")
		return
	}
	students, err := a.db.CourseStudents(id)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if students == nil {
		students = []User{}
	}
	writeJSON(w, http.StatusOK, students)
}

func (a *API) handleAssignTeacher(w http.ResponseWriter, r *http.Request, admin *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	var req struct {
		TeacherID int64 `json:"teacher_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeacherID <= 0 {
		writeErr(w, 400, "参数错误")
		return
	}
	t, err := a.db.GetUser(req.TeacherID)
	if err != nil || t.Role != "teacher" {
		writeErr(w, 400, "目标用户不是老师")
		return
	}
	if _, err := a.db.GetCourse(id); err != nil {
		writeErr(w, 404, "课程不存在")
		return
	}
	if err := a.db.SetCourseTeacher(id, req.TeacherID); err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- 封面 ----------------

func (a *API) handleUploadCover(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权操作该课程")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "请选择封面图片（≤5MB）")
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if n < 8 {
		writeErr(w, 400, "图片无效")
		return
	}
	ext, ok := imageMimeExt(http.DetectContentType(head[:n]))
	if !ok {
		writeErr(w, 400, "仅支持 png/jpg/gif/webp 图片")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, "读取失败")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 400, "图片读取失败")
		return
	}
	key := "cover/" + strconv.FormatInt(id, 10) + ext
	if err := a.stg().Put(r.Context(), key, data); err != nil {
		writeErr(w, 500, "保存失败: "+err.Error())
		return
	}
	if err := a.db.SetCourseCover(id, key); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"cover": key})
}

func (a *API) handleCourseCover(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil || c.Cover == "" {
		writeErr(w, 404, "没有封面")
		return
	}
	rc, err := a.stg().Get(r.Context(), c.Cover)
	if err != nil {
		writeErr(w, 404, "没有封面")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", imageContentType(c.Cover))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, rc)
}

// ---------------- 课程市场 ----------------

func (a *API) handleMarket(w http.ResponseWriter, r *http.Request, u *User) {
	courses, err := a.db.ListCoursesMarket(u.ID)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if courses == nil {
		courses = []Course{}
	}
	for i := range courses {
		courses[i].InviteCode = ""
	}
	writeJSON(w, http.StatusOK, courses)
}

func (a *API) handleJoinByCode(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以加入课程")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		writeErr(w, 400, "请输入邀请码")
		return
	}
	c, err := a.db.GetCourseByInviteCode(strings.ToUpper(strings.TrimSpace(req.Code)))
	if err != nil {
		writeErr(w, http.StatusNotFound, "邀请码无效")
		return
	}
	if err := a.db.AddEnrollment(c.ID, u.ID); err != nil {
		writeErr(w, 500, "加入失败: "+err.Error())
		return
	}
	c.Enrolled = true
	c.InviteCode = ""
	writeJSON(w, http.StatusOK, c)
}

func (a *API) handleMarketEnroll(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以加入课程")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil || !c.Published {
		writeErr(w, 404, "课程不存在或未上架")
		return
	}
	if err := a.db.AddEnrollment(c.ID, u.ID); err != nil {
		writeErr(w, 500, "加入失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- 课时 ----------------

func (a *API) handleListLessons(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil {
		writeErr(w, 404, "课程不存在")
		return
	}
	lessons, err := a.db.CourseLessons(id)
	if err != nil {
		writeErr(w, 500, "查询课时失败: "+err.Error())
		return
	}
	if lessons == nil {
		lessons = []Lesson{}
	}
	access := a.canViewLessons(u, c) // 未加入的学员只能看到目录标题
	if !access && !c.Published {
		writeErr(w, http.StatusForbidden, "无权访问该课程")
		return
	}
	if !access {
		for i := range lessons {
			lessons[i].Content = ""
			lessons[i].Filename = ""
			lessons[i].Size = 0
		}
	}
	a.writeLessons(w, lessons)
}

func (a *API) handleCreateLesson(w http.ResponseWriter, r *http.Request, u *User) {
	courseID, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, courseID); !ok {
		writeErr(w, http.StatusForbidden, "无权向该课程添加课时")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Chapter string `json:"chapter"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.Kind != "video" && req.Kind != "rich" && req.Kind != "doc" {
		writeErr(w, 400, "课时类型无效")
		return
	}
	title := trimTitle(req.Title)
	if title == "" {
		title = "未命名课时"
	}
	chapter := trimTitle(req.Chapter)
	id, err := a.db.CreateLesson(courseID, chapter, title, req.Kind)
	if err != nil {
		writeErr(w, 500, "创建课时失败: "+err.Error())
		return
	}
	if req.Kind == "rich" && strings.TrimSpace(req.Content) != "" {
		a.db.UpdateLessonContent(id, sanitizeHTML(req.Content))
	}
	l, err := a.db.GetLesson(id)
	if err != nil {
		writeErr(w, 500, "课时回查失败")
		return
	}
	a.writeLesson(w, l)
}

func (a *API) handleGetLesson(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canAccessCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权访问该课时")
		return
	}
	a.writeLesson(w, l)
}

func (a *API) handleUpdateLesson(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权修改该课时")
		return
	}
	var req struct {
		Title   *string `json:"title"`
		Chapter *string `json:"chapter"`
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	title, chapter := l.Title, l.Chapter
	if req.Title != nil {
		if t := trimTitle(*req.Title); t != "" {
			title = t
		}
	}
	if req.Chapter != nil {
		chapter = trimTitle(*req.Chapter)
	}
	if err := a.db.UpdateLessonMeta(id, chapter, title); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	if req.Content != nil && l.Kind == "rich" {
		html := sanitizeHTML(*req.Content)
		if len(html) > 400000 {
			writeErr(w, 400, "图文内容过长")
			return
		}
		if err := a.db.UpdateLessonContent(id, html); err != nil {
			writeErr(w, 500, "保存失败")
			return
		}
	}
	nl, _ := a.db.GetLesson(id)
	a.writeLesson(w, nl)
}

func (a *API) handleDeleteLesson(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权删除该课时")
		return
	}
	sid := strconv.FormatInt(id, 10)
	a.stg().DeletePrefix(r.Context(), "hls/"+sid)
	a.stg().DeletePrefix(r.Context(), "doc/"+sid)
	a.stg().DeletePrefix(r.Context(), "src/"+sid)
	if err := a.db.DeleteLesson(id); err != nil {
		writeErr(w, 500, "删除失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleCompleteLesson(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权操作该课时")
		return
	}
	var req struct {
		Size     int64   `json:"size"`
		Duration float64 `json:"duration"`
	}
	json.NewDecoder(r.Body).Decode(&req) // 参数可省略
	if err := a.db.CompleteLesson(id, req.Size, req.Duration); err != nil {
		writeErr(w, 500, "完成失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- 文档课时 ----------------

var docExts = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".zip":  "application/zip",
}

const docMaxBytes = 200 << 20 // 200MB

func (a *API) handleUploadDoc(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil || l.Kind != "doc" {
		writeErr(w, 404, "课时不存在")
		return
	}
	if _, ok := a.canManageCourse(u, l.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权上传")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, docMaxBytes+1<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "请选择文件（≤200MB）")
		return
	}
	defer file.Close()
	name := path.Base(header.Filename)
	ext := strings.ToLower(path.Ext(name))
	mimeType, ok := docExts[ext]
	if !ok {
		writeErr(w, 400, "仅支持 pdf / word / excel / ppt / txt / md / csv / zip")
		return
	}
	if !nameRe.MatchString("f" + ext) {
		writeErr(w, 400, "文件扩展名无效")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		writeErr(w, 400, "文件读取失败或为空")
		return
	}
	if len(data) > docMaxBytes {
		writeErr(w, 400, "文件超过 200MB")
		return
	}
	sid := strconv.FormatInt(id, 10)
	key := "doc/" + sid + "/" + "doc" + ext
	if err := a.stg().PutStream(r.Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		writeErr(w, 500, "保存失败: "+err.Error())
		return
	}
	if err := a.db.SetLessonDoc(id, key, name, mimeType, int64(len(data))); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	nl, _ := a.db.GetLesson(id)
	a.writeLesson(w, nl)
}

func (a *API) handleLessonFile(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课时 ID")
		return
	}
	l, err := a.db.GetLesson(id)
	if err != nil || l.Kind != "doc" || l.Content == "" {
		writeErr(w, 404, "文件不存在")
		return
	}
	if !a.checkSign(r, "/api/lessons/"+strconv.FormatInt(id, 10)+"/file") {
		if _, ok := a.canAccessCourse(u, l.CourseID); !ok {
			writeErr(w, http.StatusForbidden, "无权下载该文件")
			return
		}
	}
	ctype := l.Mime
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	disp := "attachment"
	if strings.HasPrefix(ctype, "application/pdf") || strings.HasPrefix(ctype, "text/") {
		disp = "inline"
	}
	fn := url.PathEscape(l.Filename)
	if l.Filename == "" {
		fn = "file" + path.Ext(l.Content)
	}
	w.Header().Set("Cache-Control", "private, max-age=600")
	a.serveStorageRange(w, r, l.Content, ctype, disp+`; filename*=UTF-8''`+fn)
}

// ---------------- 图片（聊天 / 图文） ----------------

func (a *API) handleImageUpload(w http.ResponseWriter, r *http.Request, u *User) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "请上传图片（≤10MB）")
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if n < 8 {
		writeErr(w, 400, "文件无效")
		return
	}
	ext, ok := imageMimeExt(http.DetectContentType(head[:n]))
	if !ok {
		writeErr(w, 400, "仅支持 png/jpg/gif/webp 图片")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, "读取失败")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 400, "图片读取失败")
		return
	}
	buf := make([]byte, 16)
	rand.Read(buf)
	key := "img/" + hexStr(buf) + ext
	if err := a.stg().Put(r.Context(), key, data); err != nil {
		writeErr(w, 500, "图片保存失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/files/" + key})
}

var fileKeyRe = regexp.MustCompile(`^(chat|img)/[a-f0-9]{32}\.(png|jpg|gif|webp)$`)

func (a *API) handleServeFile(w http.ResponseWriter, r *http.Request, u *User) {
	key := strings.TrimPrefix(r.URL.Path, "/files/")
	if !fileKeyRe.MatchString(key) {
		writeErr(w, 404, "文件不存在")
		return
	}
	rc, err := a.stg().Get(r.Context(), key)
	if err != nil {
		writeErr(w, 404, "文件不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", imageContentType(key))
	w.Header().Set("Cache-Control", "private, max-age=604800")
	io.Copy(w, rc)
}

// hexStr 小写十六进制编码（聊天/图文图片文件名）
func hexStr(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0xf]
	}
	return string(out)
}
