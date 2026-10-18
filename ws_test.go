package main

import (
	"bytes"
	"encoding/json"
	"io"
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

// 端到端测试：真实 HTTP + WebSocket，覆盖 Cookie 安全属性/CSRF/安全头/限流/RBAC/邀请码/聊天/通话信令

var testBase string

func newTestAPI(t *testing.T) (*API, http.Handler) {
	t.Helper()
	cfg := &Config{
		Port: "0", AdminPassword: "admin-pass-123", Secret: "test-secret",
		DataDir: t.TempDir(), StorageType: "local", AllowRegister: true,
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
// 在线状态变化会随时广播 presence，断言聊天/信令消息时需要跳过。
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

	// 未匹配的 API 路径返回 404，而不是回退到前端页面
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

	// ---------- RBAC ----------
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

	// ---------- 课程 / 邀请码 ----------
	var course Course
	decode(t, postJSON(t, tc, "/api/courses", map[string]string{"title": "Go 语言入门"}), &course)
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
	respMe, err := tc.Get(testBase + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, respMe, &me)
	if me.Username != "teacher1" || me.Role != "teacher" {
		t.Fatalf("/api/me 异常: %+v", me)
	}

	var students []User
	respSt, err := tc.Get(testBase + "/api/courses/" + strconv.FormatInt(course.ID, 10) + "/students")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, respSt, &students)
	if len(students) != 1 || students[0].ID != student1.ID {
		t.Fatalf("课程学员列表异常: %+v", students)
	}

	var stContacts []struct {
		User
		Unread int64 `json:"unread"`
	}
	respC, err := sc1.Get(testBase + "/api/contacts")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, respC, &stContacts)
	if len(stContacts) != 1 || stContacts[0].ID != teacher.ID {
		t.Fatalf("学员联系人异常: %+v", stContacts)
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

	// ---------- 视频权限 ----------
	var vid struct {
		ID int64 `json:"id"`
	}
	decode(t, postJSON(t, tc, "/api/videos", map[string]any{"course_id": course.ID, "title": "第1课"}), &vid)
	r = postJSON(t, sc2, "/api/videos/"+strconv.FormatInt(vid.ID, 10)+"/complete", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("未加入学员不应能操作视频: %d", r.StatusCode)
	}
	r = postJSON(t, sc1, "/api/videos/"+strconv.FormatInt(vid.ID, 10)+"/complete", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("已加入学员也不应能管理视频: %d", r.StatusCode)
	}

	// ---------- WebSocket 聊天 ----------
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
	wss2 := dialWS(t, sc2)
	defer wss2.Close()
	readMsg(t, wss2, 3*time.Second) // presence
	wss2.WriteJSON(wsMsg{Type: "chat.send", To: teacher.ID, Msg: &chatPayload{Type: "text", Content: "hi"}})
	m2 := readMsgNotPresence(t, wss2, 3*time.Second)
	if m2.Type != "error" {
		t.Fatal("未加入课程的学员不应能与老师私聊")
	}

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
	// 挂断
	wss.WriteJSON(wsMsg{Type: "call.end", To: teacher.ID})
	endMsg := readMsgNotPresence(t, wst, 3*time.Second)
	if endMsg.Type != "call.end" {
		t.Fatal("老师应收到挂断通知")
	}

	// ---------- 聊天历史 / 未读 ----------
	var hist []Message
	respH, err := tc.Get(testBase + "/api/chat/" + strconv.FormatInt(student1.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	decode(t, respH, &hist)
	if len(hist) == 0 || hist[0].Content != "老师好 🙂" {
		t.Fatalf("聊天历史异常: %+v", hist)
	}
	respH.Body.Close()
}
