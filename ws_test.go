package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

// 端到端测试：真实 HTTP + WebSocket，覆盖 Cookie 安全属性/CSRF/安全头/限流/RBAC/邀请码/
// 课程市场/课时（视频/图文/文档）/群聊（引用时间点）/私聊/通话信令/注册模式

var testBase string

func newTestAPI(t *testing.T) (*API, http.Handler) {
	t.Helper()
	cfg := &Config{
		Port: "0", AdminPassword: "admin-pass-123", Secret: "test-secret",
		DataDir: t.TempDir(), StorageType: "local", AllowRegister: true,
		SiteName: "课程中心", RegisterMode: "open",
	}
	st, err := NewStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.InitSettings(cfg.SiteName, cfg.RegisterMode, ""); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin-pass-123"), bcrypt.DefaultCost)
	if _, err := db.CreateUser(&User{Username: "admin", PasswordHash: string(hash), DisplayName: "管理员", Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	api := &API{cfg: cfg, st: st, db: db, limiter: newLoginLimiter()}
	api.hub = newHub(api)
	mux := http.NewServeMux()
	api.routes(mux)
	return api, withSecureHeaders(mux)
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func postJSON(t *testing.T, c *http.Client, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := c.Post(testBase+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func putJSON(t *testing.T, c *http.Client, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, testBase+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func getJSON(t *testing.T, c *http.Client, path string, v any) {
	t.Helper()
	resp, err := c.Get(testBase + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	decode(t, resp, v)
}

func uploadFile(t *testing.T, c *http.Client, path, filename string, content []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, testBase+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("upload %s: %v", path, err)
	}
	return resp
}

func login(t *testing.T, username, password string) *http.Client {
	t.Helper()
	c := newClient()
	resp := postJSON(t, c, "/api/login", map[string]string{"username": username, "password": password})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login %s: status %d", username, resp.StatusCode)
	}
	return c
}

func dialWS(t *testing.T, c *http.Client) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(testBase)
	hdr := http.Header{}
	for _, ck := range c.Jar.Cookies(u) {
		hdr.Add("Cookie", ck.Name+"="+ck.Value)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+u.Host+"/ws", hdr)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return ws
}

func readMsg(t *testing.T, ws *websocket.Conn, timeout time.Duration) serverMsg {
	t.Helper()
	type res struct {
		m   serverMsg
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var m serverMsg
		err := ws.ReadJSON(&m)
		ch <- res{m, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read ws: %v", r.err)
		}
		return r.m
	case <-time.After(timeout):
		t.Fatal("read ws timeout")
		return serverMsg{}
	}
}

// readMsgNotPresence 读取下一条非 presence 消息。
func readMsgNotPresence(t *testing.T, ws *websocket.Conn, timeout time.Duration) serverMsg {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		m := readMsg(t, ws, time.Until(deadline))
		if m.Type != "presence" {
			return m
		}
	}
}

// wsReader 后台持续读取连接，用于「一段时间内不应收到消息」这类断言：
// gorilla 的读超时会让连接不可用，所以不能在主 goroutine 上直接读。
type wsReader struct {
	ch chan serverMsg
}

func startWSReader(ws *websocket.Conn) *wsReader {
	r := &wsReader{ch: make(chan serverMsg, 256)}
	go func() {
		defer close(r.ch)
		for {
			var m serverMsg
			if err := ws.ReadJSON(&m); err != nil {
				return
			}
			r.ch <- m
		}
	}()
	return r
}

func (r *wsReader) next(t *testing.T, timeout time.Duration) serverMsg {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m, ok := <-r.ch:
			if !ok {
				t.Fatal("连接已断开，读不到消息")
			}
			if m.Type == "presence" {
				continue
			}
			return m
		case <-deadline:
			t.Fatalf("等待消息超时（%s）", timeout)
			return serverMsg{}
		}
	}
}

func (r *wsReader) none(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m, ok := <-r.ch:
			if !ok {
				return
			}
			if m.Type == "presence" {
				continue
			}
			t.Fatalf("不应收到消息，却收到 %s（%+v）", m.Type, m)
		case <-deadline:
			return
		}
	}
}

func TestEndToEnd(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	// ---------- Cookie 安全属性 ----------
	c := newClient()
	resp := postJSON(t, c, "/api/login", map[string]string{"username": "admin", "password": "admin-pass-123"})
	var sessionCookie string
	for _, v := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(v, "coursehub_session") {
			sessionCookie = v
		}
	}
	resp.Body.Close()
	if sessionCookie == "" || !strings.Contains(sessionCookie, "HttpOnly") || !strings.Contains(sessionCookie, "SameSite=Lax") {
		t.Fatalf("session cookie 缺少 HttpOnly/SameSite: %q", sessionCookie)
	}

	// ---------- CSRF：跨站 Origin 拒绝 ----------
	req, _ := http.NewRequest("POST", testBase+"/api/courses", strings.NewReader(`{"title":"evil"}`))
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Content-Type", "application/json")
	resp2, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("跨站 Origin 应被拒，实际 %d", resp2.StatusCode)
	}

	// ---------- 安全响应头 ----------
	resp3, _ := http.Get(testBase + "/api/config")
	if resp3.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("缺少 X-Frame-Options")
	}
	resp3.Body.Close()

	resp404, _ := http.Get(testBase + "/api/not-exist")
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("未知 API 应返回 404，实际 %d", resp404.StatusCode)
	}

	// ---------- 登录限流（10 次失败后 429） ----------
	lim := newClient()
	saw429 := false
	for i := 0; i < 12; i++ {
		r := postJSON(t, lim, "/api/login", map[string]string{"username": "admin", "password": "wrong"})
		r.Body.Close()
		if r.StatusCode == http.StatusTooManyRequests {
			saw429 = true
			break
		}
	}
	if !saw429 {
		t.Fatal("连续错误密码未触发限流")
	}

	// ---------- 管理员建老师/学员 ----------
	var teacher User
	decode(t, postJSON(t, c, "/api/users", map[string]any{
		"username": "teacher1", "password": "teach-pass", "display_name": "王老师", "role": "teacher"}), &teacher)
	if teacher.ID == 0 {
		t.Fatal("创建老师失败")
	}
	var student1 User
	decode(t, postJSON(t, c, "/api/users", map[string]any{
		"username": "student1", "password": "stu-pass", "display_name": "小明", "role": "student"}), &student1)
	var student2 User
	decode(t, postJSON(t, c, "/api/users", map[string]any{
		"username": "student2", "password": "stu-pass", "display_name": "小红", "role": "student"}), &student2)

	tc := login(t, "teacher1", "teach-pass")
	sc1 := login(t, "student1", "stu-pass")
	sc2 := login(t, "student2", "stu-pass")

	// ---------- RBAC：学员不能建课、不能进用户管理 ----------
	r := postJSON(t, sc1, "/api/courses", map[string]string{"title": "hack"})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("学员不应能创建课程")
	}
	r = postJSON(t, sc1, "/api/users", map[string]any{"username": "x2", "password": "123456", "role": "admin"})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("学员不应能访问用户管理")
	}
	// 学员不能上传课时
	r = postJSON(t, sc1, "/api/courses/1/lessons", map[string]any{"title": "x", "kind": "video"})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden && r.StatusCode != http.StatusNotFound {
		t.Fatalf("学员不应能添加课时: %d", r.StatusCode)
	}

	// ---------- 老师建课 / 邀请码 ----------
	var course Course
	decode(t, postJSON(t, tc, "/api/courses", map[string]string{
		"title": "Go 语言入门", "description": "从零开始学 Go", "category": "编程"}), &course)
	if course.InviteCode == "" || course.TeacherName != "王老师" {
		t.Fatalf("课程数据异常: %+v", course)
	}
	r = postJSON(t, sc1, "/api/join", map[string]string{"code": "WRONG123"})
	r.Body.Close()
	if r.StatusCode == 200 {
		t.Fatal("错误邀请码不应加入成功")
	}
	var joined Course
	decode(t, postJSON(t, sc1, "/api/join", map[string]string{"code": course.InviteCode}), &joined)
	if joined.ID != course.ID {
		t.Fatal("凭邀请码加入失败")
	}

	// ---------- 当前用户 / 老师查看学员 / 聊天联系人 ----------
	var me User
	getJSON(t, tc, "/api/me", &me)
	if me.Username != "teacher1" || me.Role != "teacher" {
		t.Fatalf("/api/me 异常: %+v", me)
	}
	var students []User
	getJSON(t, tc, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/students", &students)
	if len(students) != 1 || students[0].ID != student1.ID {
		t.Fatalf("课程学员列表异常: %+v", students)
	}
	var stContacts []struct {
		User
		Unread int64 `json:"unread"`
	}
	getJSON(t, sc1, "/api/contacts", &stContacts)
	if len(stContacts) != 1 || stContacts[0].ID != teacher.ID {
		t.Fatalf("学员联系人异常: %+v", stContacts)
	}

	// ---------- 课时：视频 / 图文 / 文档 ----------
	var video Lesson
	decode(t, postJSON(t, tc, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/lessons",
		map[string]any{"title": "第1课 变量", "kind": "video", "chapter": "第一章"}), &video)
	if video.Kind != "video" || video.Status != "uploading" {
		t.Fatalf("视频课时异常: %+v", video)
	}
	r = postJSON(t, sc2, "/api/lessons/"+strconv.FormatInt(video.ID, 10)+"/complete", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("未加入学员不应能操作课时: %d", r.StatusCode)
	}
	r = postJSON(t, sc1, "/api/lessons/"+strconv.FormatInt(video.ID, 10)+"/complete", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("已加入学员也不应能管理课时: %d", r.StatusCode)
	}
	decode(t, postJSON(t, tc, "/api/lessons/"+strconv.FormatInt(video.ID, 10)+"/complete",
		map[string]any{"size": 1024, "duration": 360}), &map[string]any{})

	// 图文课时：HTML 必须被白名单过滤
	var rich Lesson
	decode(t, postJSON(t, tc, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/lessons", map[string]any{
		"title": "讲义", "kind": "rich",
		"content": `<p style="color:red" onclick="alert(1)">你好<b>世界</b></p><script>alert(1)</script>`}), &rich)
	if strings.Contains(rich.Content, "script") || strings.Contains(rich.Content, "onclick") {
		t.Fatalf("图文内容未被过滤: %s", rich.Content)
	}
	if !strings.Contains(rich.Content, "你好") || !strings.Contains(rich.Content, "<b>") {
		t.Fatalf("图文内容被过度过滤: %s", rich.Content)
	}

	// 文档课时：上传 + 权限
	var doc Lesson
	decode(t, postJSON(t, tc, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/lessons",
		map[string]any{"title": "课件", "kind": "doc"}), &doc)
	respDoc := uploadFile(t, tc, "/api/lessons/"+strconv.FormatInt(doc.ID, 10)+"/doc", "outline.txt", []byte("hello doc"))
	var uploaded Lesson
	decode(t, respDoc, &uploaded)
	if uploaded.Status != "ready" || uploaded.Filename != "outline.txt" {
		t.Fatalf("文档上传异常: %+v", uploaded)
	}
	respF, err := sc1.Get(testBase + "/api/lessons/" + strconv.FormatInt(doc.ID, 10) + "/file")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(respF.Body)
	respF.Body.Close()
	if respF.StatusCode != 200 || string(body) != "hello doc" {
		t.Fatalf("文档下载异常: %d %q", respF.StatusCode, body)
	}
	respF2, _ := sc2.Get(testBase + "/api/lessons/" + strconv.FormatInt(doc.ID, 10) + "/file")
	respF2.Body.Close()
	if respF2.StatusCode != http.StatusForbidden {
		t.Fatalf("未加入学员不应能下载文档: %d", respF2.StatusCode)
	}

	// 目录：未加入学员（且课程未上架）应 403
	respL, _ := sc2.Get(testBase + "/api/courses/" + strconv.FormatInt(course.ID, 10) + "/lessons")
	respL.Body.Close()
	if respL.StatusCode != http.StatusForbidden {
		t.Fatalf("未加入学员不应能看未上架课程目录: %d", respL.StatusCode)
	}

	// ---------- 课程市场：上架 → 学员自主加入 ----------
	decode(t, postJSON(t, tc, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/publish",
		map[string]any{"published": true}), &map[string]any{})
	var market []Course
	getJSON(t, sc2, "/api/market", &market)
	found := false
	for _, mc := range market {
		if mc.ID == course.ID && mc.Published && !mc.Enrolled {
			found = true
		}
	}
	if !found {
		t.Fatalf("市场列表异常: %+v", market)
	}
	// 未加入也能看到目录（预览），但拿不到内容
	var preview []Lesson
	getJSON(t, sc2, "/api/courses/"+strconv.FormatInt(course.ID, 10)+"/lessons", &preview)
	for _, l := range preview {
		if l.Content != "" || l.Filename != "" {
			t.Fatalf("预览目录不应包含内容: %+v", l)
		}
	}
	r = postJSON(t, sc2, "/api/market/"+strconv.FormatInt(course.ID, 10)+"/enroll", map[string]any{})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("市场加入课程失败: %d", r.StatusCode)
	}
	var myCourses []Course
	getJSON(t, sc2, "/api/courses", &myCourses)
	if len(myCourses) != 1 || myCourses[0].ID != course.ID {
		t.Fatalf("学员课程列表异常: %+v", myCourses)
	}

	// ---------- 禁用账号不可登录；管理员不能禁用自己 ----------
	var dis User
	decode(t, postJSON(t, c, "/api/users", map[string]any{
		"username": "disabled_one", "password": "pass123", "role": "student"}), &dis)
	r = postJSON(t, c, "/api/users/"+strconv.FormatInt(dis.ID, 10)+"/status", map[string]string{"status": "disabled"})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("禁用账号失败: %d", r.StatusCode)
	}
	bad := newClient()
	respDis := postJSON(t, bad, "/api/login", map[string]string{"username": "disabled_one", "password": "pass123"})
	respDis.Body.Close()
	if respDis.StatusCode != http.StatusForbidden {
		t.Fatalf("已禁用账号不应能登录: %d", respDis.StatusCode)
	}
	r = postJSON(t, c, "/api/users/1/status", map[string]string{"status": "disabled"})
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("管理员不应能禁用自己: %d", r.StatusCode)
	}

	// ---------- WebSocket 私聊 ----------
	wst := dialWS(t, tc)
	wss := dialWS(t, sc1)
	defer wst.Close()
	defer wss.Close()

	m1 := readMsg(t, wst, 3*time.Second)
	if m1.Type != "presence" {
		t.Fatalf("期望 presence，得到 %s", m1.Type)
	}

	if err := wss.WriteJSON(wsMsg{Type: "chat.send", To: teacher.ID, Msg: &chatPayload{Type: "text", Content: "老师好 🙂"}}); err != nil {
		t.Fatal(err)
	}
	mt := readMsgNotPresence(t, wst, 3*time.Second)
	if mt.Type != "chat.new" || mt.Msg == nil || mt.Msg.Content != "老师好 🙂" {
		t.Fatalf("老师应收到消息，得到 %s %+v", mt.Type, mt.Msg)
	}
	ms := readMsgNotPresence(t, wss, 3*time.Second)
	if ms.Type != "chat.sent" {
		t.Fatalf("发送方应收到回执，得到 %s", ms.Type)
	}

	// 未加入课程的学员不能与老师私聊
	var student3 User
	decode(t, postJSON(t, c, "/api/users", map[string]any{
		"username": "student3", "password": "stu-pass", "display_name": "小刚", "role": "student"}), &student3)
	sc3 := login(t, "student3", "stu-pass")
	wss3 := dialWS(t, sc3)
	defer wss3.Close()
	readMsg(t, wss3, 3*time.Second) // presence
	wss3.WriteJSON(wsMsg{Type: "chat.send", To: teacher.ID, Msg: &chatPayload{Type: "text", Content: "hi"}})
	m2 := readMsgNotPresence(t, wss3, 3*time.Second)
	if m2.Type != "error" {
		t.Fatal("未加入课程的学员不应能与老师私聊")
	}

	// ---------- 群聊：引用视频时间点 ----------
	wss.WriteJSON(wsMsg{Type: "group.send", Group: course.ID,
		Msg: &chatPayload{Type: "text", Content: "这里没听懂"},
		Ref: &msgRef{LessonID: video.ID, Time: 123.5}})
	gt := readMsgNotPresence(t, wst, 3*time.Second)
	if gt.Type != "group.new" || gt.GMsg == nil || gt.GMsg.Content != "这里没听懂" {
		t.Fatalf("老师应收到群消息，得到 %s %+v", gt.Type, gt.GMsg)
	}
	if gt.GMsg.RefLessonID != video.ID || gt.GMsg.RefTime != 123.5 || gt.GMsg.RefTitle != "第1课 变量" {
		t.Fatalf("群消息引用异常: %+v", gt.GMsg)
	}
	gs := readMsgNotPresence(t, wss, 3*time.Second)
	if gs.Type != "group.sent" || gs.GMsg == nil {
		t.Fatalf("发送方应收到群消息回执，得到 %s", gs.Type)
	}
	var groups []GroupSummary
	getJSON(t, tc, "/api/groups", &groups)
	if len(groups) != 1 || groups[0].CourseID != course.ID || groups[0].Unread < 1 {
		t.Fatalf("群列表未读异常: %+v", groups)
	}
	var gh []GroupMessage
	getJSON(t, tc, "/api/groups/"+strconv.FormatInt(course.ID, 10)+"/messages", &gh)
	if len(gh) != 1 || gh[0].Content != "这里没听懂" || gh[0].RefTime != 123.5 {
		t.Fatalf("群历史异常: %+v", gh)
	}
	// 群消息只对本课程成员开放
	wss2 := dialWS(t, sc2) // student2 已加入，可正常访问
	defer wss2.Close()
	readMsg(t, wss2, 3*time.Second) // presence

	// ---------- 通话信令中继 ----------
	offer := `{"type":"offer","sdp":"v=0 fake"}`
	wst.WriteJSON(wsMsg{Type: "call.offer", To: student1.ID, SDP: offer, Video: true})
	mc := readMsgNotPresence(t, wss, 3*time.Second)
	if mc.Type != "call.offer" || mc.SDP != offer || !mc.Video {
		t.Fatalf("学员应收到视频通话 offer: %+v", mc)
	}
	wst.WriteJSON(wsMsg{Type: "call.ice", To: student1.ID,
		Candidate: json.RawMessage(`{"candidate":"candidate:1 1 UDP 1 127.0.0.1 5000 typ host"}`)})
	mi := readMsgNotPresence(t, wss, 3*time.Second)
	if mi.Type != "call.ice" || !strings.Contains(string(mi.Candidate), "127.0.0.1") {
		t.Fatalf("学员应收到 ICE 候选: %+v", mi)
	}
	// 老师已占线：学员2 呼叫应得到 busy
	wss2.WriteJSON(wsMsg{Type: "call.offer", To: teacher.ID, SDP: offer})
	mb := readMsgNotPresence(t, wss2, 3*time.Second)
	if mb.Type != "call.busy" {
		t.Fatal("通话中的老师应返回 busy")
	}
	wss.WriteJSON(wsMsg{Type: "call.end", To: teacher.ID})
	endMsg := readMsgNotPresence(t, wst, 3*time.Second)
	if endMsg.Type != "call.end" {
		t.Fatal("老师应收到挂断通知")
	}

	// ---------- 通话断线宽限：信令抖动 / 多标签页不再误挂断 ----------
	oldGrace := callEndGrace
	callEndGrace = 300 * time.Millisecond
	defer func() { callEndGrace = oldGrace }()
	tr := startWSReader(wst) // 之后 wst 只由后台 reader 读取

	// 老师呼叫 student1（服务端随即把双方标记为通话中）
	wst.WriteJSON(wsMsg{Type: "call.offer", To: student1.ID, SDP: offer, Video: true})
	if mo := readMsgNotPresence(t, wss, 3*time.Second); mo.Type != "call.offer" || !mo.Video {
		t.Fatalf("student1 应收到视频通话邀请: %s", mo.Type)
	}
	tr.none(t, 150*time.Millisecond) // offer 不应回传给发起方

	// 1) 同账号第二个连接（多标签页）关闭，不能挂断正在进行的通话
	dup := dialWS(t, sc1)
	readMsg(t, dup, 3*time.Second) // presence
	dup.Close()
	tr.none(t, 900*time.Millisecond)

	// 2) 掉线后在宽限期内重连，同样不能挂断
	wss.Close()
	wss = dialWS(t, sc1)
	readMsg(t, wss, 3*time.Second) // presence
	tr.none(t, 900*time.Millisecond)

	// 3) 真正掉线（宽限期内没重连）才通知对端「对方已断线」，并释放占线状态
	wss.Close()
	gone := tr.next(t, 3*time.Second)
	if gone.Type != "call.end" || gone.Message != "对方已断线" {
		t.Fatalf("断线超时后应收到 call.end/对方已断线，得到 %s %q", gone.Type, gone.Message)
	}
	wssRe := dialWS(t, sc1)
	defer wssRe.Close()
	readMsg(t, wssRe, 3*time.Second) // presence
	wst.WriteJSON(wsMsg{Type: "call.offer", To: student1.ID, SDP: offer})
	if mo := readMsgNotPresence(t, wssRe, 3*time.Second); mo.Type != "call.offer" {
		t.Fatalf("通话结束后应能重新呼叫，得到 %s", mo.Type)
	}
	tr.none(t, 200*time.Millisecond)
	wst.WriteJSON(wsMsg{Type: "call.end", To: student1.ID}) // 收尾：释放占线

	// ---------- 私聊历史 / 未读 ----------
	var hist []Message
	getJSON(t, tc, "/api/chat/"+strconv.FormatInt(student1.ID, 10), &hist)
	if len(hist) == 0 || hist[0].Content != "老师好 🙂" {
		t.Fatalf("聊天历史异常: %+v", hist)
	}

	// ---------- 注册模式：开放 / 私有 / 邀请码 ----------
	var pubCfg map[string]any
	getJSON(t, newClient(), "/api/config", &pubCfg)
	if pubCfg["register_mode"] != "open" || pubCfg["allow_register"] != true {
		t.Fatalf("默认注册配置异常: %+v", pubCfg)
	}
	decode(t, postJSON(t, newClient(), "/api/register", map[string]any{
		"username": "self_open", "password": "pass123"}), &User{})

	var settings Settings
	decode(t, putJSON(t, c, "/api/settings", map[string]any{"register_mode": "closed"}), &settings)
	if settings.RegisterMode != "closed" {
		t.Fatalf("设置注册模式失败: %+v", settings)
	}
	r = postJSON(t, newClient(), "/api/register", map[string]any{"username": "self_closed", "password": "pass123"})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("关闭注册后不应能注册: %d", r.StatusCode)
	}

	var inv Settings
	decode(t, putJSON(t, c, "/api/settings", map[string]any{"register_mode": "invite"}), &inv)
	if inv.RegisterMode != "invite" || inv.OrgInviteCode == "" {
		t.Fatalf("切换邀请码注册失败: %+v", inv)
	}
	r = postJSON(t, newClient(), "/api/register", map[string]any{
		"username": "self_nocode", "password": "pass123", "invite_code": "WRONG"})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("邀请码错误不应能注册: %d", r.StatusCode)
	}
	decode(t, postJSON(t, newClient(), "/api/register", map[string]any{
		"username": "self_invited", "password": "pass123", "invite_code": inv.OrgInviteCode}), &User{})

	// 非法注册模式
	r = putJSON(t, c, "/api/settings", map[string]any{"register_mode": "whatever"})
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法注册模式应 400，实际 %d", r.StatusCode)
	}
}
