package main

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type API struct {
	cfg     *Config
	st      Storage
	db      *Store
	hub     *Hub
	limiter *loginLimiter
}

func (a *API) routes(mux *http.ServeMux) {
	// 公开
	mux.HandleFunc("POST /api/register", a.handleRegister)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("GET /api/config", a.handleConfig)

	// 登录用户
	mux.HandleFunc("GET /api/me", a.requireUser(a.handleMe))
	mux.HandleFunc("POST /api/logout", a.requireUser(a.handleLogout))

	// 课程
	mux.HandleFunc("GET /api/courses", a.requireUser(a.handleListCourses))
	mux.HandleFunc("POST /api/courses", a.requireUser(a.handleCreateCourse))
	mux.HandleFunc("DELETE /api/courses/{id}", a.requireUser(a.handleDeleteCourse))
	mux.HandleFunc("GET /api/courses/{id}/videos", a.requireUser(a.handleListVideos))
	mux.HandleFunc("POST /api/courses/{id}/join", a.requireUser(a.handleJoinCourse))
	mux.HandleFunc("POST /api/join", a.requireUser(a.handleJoinByCode))
	mux.HandleFunc("POST /api/courses/{id}/invite", a.requireUser(a.handleNewInviteCode))
	mux.HandleFunc("GET /api/courses/{id}/students", a.requireUser(a.handleCourseStudents))
	mux.HandleFunc("POST /api/courses/{id}/teacher", a.requireRole(a.handleAssignTeacher, "admin"))

	// 视频
	mux.HandleFunc("POST /api/videos", a.requireUser(a.handleCreateVideo))
	mux.HandleFunc("GET /api/videos/{id}", a.requireUser(a.handleGetVideo))
	mux.HandleFunc("DELETE /api/videos/{id}", a.requireUser(a.handleDeleteVideo))
	mux.HandleFunc("POST /api/videos/{id}/complete", a.requireUser(a.handleCompleteVideo))
	mux.HandleFunc("PUT /api/videos/{id}/m3u8", a.requireUser(a.handlePutM3U8))
	mux.HandleFunc("PUT /api/videos/{id}/segments/{name}", a.requireUser(a.handlePutSegment))

	// 媒体（Cookie 鉴权 + 课程访问控制）
	mux.HandleFunc("GET /hls/{id}/{file}", a.requireUser(a.handleHLS))
	mux.HandleFunc("GET /files/", a.requireUser(a.handleServeFile))

	// 聊天 / 在线
	mux.HandleFunc("GET /api/contacts", a.requireUser(a.handleContacts))
	mux.HandleFunc("GET /api/chat/{peer}", a.requireUser(a.handleChatHistory))
	mux.HandleFunc("POST /api/chat/upload", a.requireUser(a.handleChatUpload))
	mux.HandleFunc("GET /api/ice", a.requireUser(a.handleIce))
	mux.HandleFunc("GET /ws", a.handleWS)

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
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func videoKey(id int64, file string) string {
	return "hls/" + strconv.FormatInt(id, 10) + "/" + file
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
	// 邀请码只发给老师/管理员
	for i := range courses {
		if u.Role == "student" {
			courses[i].InviteCode = ""
		}
	}
	writeJSON(w, http.StatusOK, courses)
}

func (a *API) handleCreateCourse(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role == "student" {
		writeErr(w, http.StatusForbidden, "学员不能创建课程")
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		TeacherID   int64  `json:"teacher_id"` // 仅管理员创建时指定任课老师
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeErr(w, 400, "课程名称不能为空")
		return
	}
	teacherID := u.ID
	if u.Role == "admin" {
		if req.TeacherID > 0 {
			t, err := a.db.GetUser(req.TeacherID)
			if err != nil || t.Role != "teacher" {
				writeErr(w, 400, "指定的任课老师不存在或不是老师")
				return
			}
			teacherID = req.TeacherID
		}
		// 管理员未指定老师时，归到管理员名下
	}
	id, err := a.db.CreateCourse(teacherID, strings.TrimSpace(req.Title), strings.TrimSpace(req.Description), genInviteCode())
	if err != nil {
		writeErr(w, 500, "创建课程失败: "+err.Error())
		return
	}
	c, err := a.db.GetCourse(id)
	if err != nil {
		writeErr(w, 500, "课程回查失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) handleDeleteCourse(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权删除该课程")
		return
	}
	ids, err := a.db.VideoIDsOfCourse(id)
	if err != nil {
		writeErr(w, 500, "查询视频失败: "+err.Error())
		return
	}
	for _, vid := range ids {
		a.st.DeletePrefix(r.Context(), "hls/"+strconv.FormatInt(vid, 10))
	}
	if err := a.db.DeleteCourse(id); err != nil {
		writeErr(w, 500, "删除课程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleJoinCourse(w http.ResponseWriter, r *http.Request, u *User) {
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
	if err != nil {
		writeErr(w, 404, "课程不存在")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeErr(w, 400, "请输入邀请码")
		return
	}
	if req.Code != c.InviteCode {
		writeErr(w, http.StatusForbidden, "邀请码错误")
		return
	}
	if err := a.db.AddEnrollment(id, u.ID); err != nil {
		writeErr(w, 500, "加入失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleJoinByCode(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以加入课程")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "请输入邀请码")
		return
	}
	c, err := a.db.GetCourseByInviteCode(strings.TrimSpace(req.Code))
	if err != nil {
		writeErr(w, http.StatusNotFound, "邀请码无效")
		return
	}
	if err := a.db.AddEnrollment(c.ID, u.ID); err != nil {
		writeErr(w, 500, "加入失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
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

// ---------------- 视频 ----------------

func (a *API) handleListVideos(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canAccessCourse(u, id); !ok {
		writeErr(w, http.StatusForbidden, "无权访问该课程")
		return
	}
	videos, err := a.db.CourseVideos(id)
	if err != nil {
		writeErr(w, 500, "查询视频失败: "+err.Error())
		return
	}
	if videos == nil {
		videos = []Video{}
	}
	writeJSON(w, http.StatusOK, videos)
}

func (a *API) handleCreateVideo(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		CourseID int64  `json:"course_id"`
		Title    string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CourseID <= 0 {
		writeErr(w, 400, "参数错误")
		return
	}
	if _, ok := a.canManageCourse(u, req.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权向该课程上传视频")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "未命名视频"
	}
	id, err := a.db.CreateVideo(req.CourseID, strings.TrimSpace(req.Title))
	if err != nil {
		writeErr(w, 500, "创建视频失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (a *API) handleGetVideo(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canAccessCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权访问该视频")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleDeleteVideo(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canManageCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权删除该视频")
		return
	}
	a.st.DeletePrefix(r.Context(), "hls/"+strconv.FormatInt(id, 10))
	if err := a.db.DeleteVideo(id); err != nil {
		writeErr(w, 500, "删除视频失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleCompleteVideo(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canManageCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权操作该视频")
		return
	}
	var req struct {
		Size     int64   `json:"size"`
		Duration float64 `json:"duration"`
	}
	json.NewDecoder(r.Body).Decode(&req) // 参数可省略
	if err := a.db.CompleteVideo(id, req.Size, req.Duration); err != nil {
		writeErr(w, 500, "完成视频失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- 上传 ----------------

func (a *API) handlePutM3U8(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canManageCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权上传")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil || len(body) == 0 || !strings.Contains(string(body), "#EXTM3U") {
		writeErr(w, 400, "m3u8 内容无效")
		return
	}
	if err := a.st.Put(r.Context(), videoKey(id, "index.m3u8"), body); err != nil {
		writeErr(w, 500, "保存 m3u8 失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handlePutSegment(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canManageCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权上传")
		return
	}
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		writeErr(w, 400, "分片文件名无效")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<20))
	if err != nil || len(body) == 0 {
		writeErr(w, 400, "分片内容为空或超出大小限制")
		return
	}
	if err := a.st.Put(r.Context(), videoKey(id, name), body); err != nil {
		writeErr(w, 500, "保存分片失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- HLS 播放代理（Cookie 鉴权 + 课程访问控制） ----------------

func (a *API) handleHLS(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的视频 ID")
		return
	}
	v, err := a.db.GetVideo(id)
	if err != nil {
		writeErr(w, 404, "视频不存在")
		return
	}
	if _, ok := a.canAccessCourse(u, v.CourseID); !ok {
		writeErr(w, http.StatusForbidden, "无权观看该视频")
		return
	}
	file := r.PathValue("file")
	if !nameRe.MatchString(file) {
		writeErr(w, 400, "文件名无效")
		return
	}
	rc, err := a.st.Get(r.Context(), videoKey(id, file))
	if err != nil {
		writeErr(w, 404, "文件不存在")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentTypeOf(file))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("HLS 代理传输中断: id=%d file=%s err=%v", id, file, err)
	}
}
