package main

// 严格模式 / 监考 / 语音评语：端到端测试

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// uploadAudioPart 上传带正确 Content-Type 的音频（模拟浏览器录音 Blob）
func uploadAudioPart(t *testing.T, c *http.Client, path string, content []byte, contentType string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if strings.HasSuffix(path, "/files") {
		if err := mw.WriteField("kind", "audio"); err != nil {
			t.Fatal(err)
		}
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="feedback.webm"`)
	h.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(h)
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

func TestStrictProctorFlow(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "proctor_teacher", "password": "teach-pass", "display_name": "监考老师", "role": "teacher"}), 200, nil)
	var stuUser User
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "proctor_stu", "password": "stu-pass", "display_name": "小明", "role": "student"}), 200, &stuUser)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "proctor_stu2", "password": "stu-pass", "display_name": "小红", "role": "student"}), 200, nil)
	teacher := login(t, "proctor_teacher", "teach-pass")
	student := login(t, "proctor_stu", "stu-pass")
	student2 := login(t, "proctor_stu2", "stu-pass")

	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "严格监考测试课"}), 200, &course)
	decodeAs(t, postJSON(t, student, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)

	// 非法监考配置被拒绝
	r := postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "坏配置", "proctor": map[string]any{"mode": "strict", "max_exits": 99},
	})
	if r.StatusCode != 400 {
		t.Fatalf("max_exits 越界应 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()
	r = postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "坏模式", "proctor": map[string]any{"mode": "hack"},
	})
	if r.StatusCode != 400 {
		t.Fatalf("非法监考模式应 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 建一份严格模式考卷：限时 30 分钟，允许离开 1 次
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "第一单元严格小测", "kind": "quiz", "subject": "english", "published": true,
		"duration": 30,
		"proctor": map[string]any{
			"mode": "strict", "max_exits": 1,
			"require_fullscreen": true, "require_camera": false, "require_screen": false,
		},
		"questions": []map[string]any{
			{"type": "choice", "stem": "“苹果”的英文是？", "options": []string{"apple", "banana"}, "answer": "A", "score": 5},
		},
	}), 201, &created)
	if created.Proctor.Mode != "strict" || created.Proctor.MaxExits != 1 || !created.Proctor.RequireFullscreen {
		t.Fatalf("严格模式配置未保存: %+v", created.Proctor)
	}
	pid := itoa(created.ID)

	// 未开始答题直接交卷 → 拒绝
	r = postJSON(t, student, "/api/practices/"+pid+"/submit", map[string]any{"answers": []any{}})
	if r.StatusCode != 400 {
		t.Fatalf("严格模式未开始就交卷应 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()
	// 未加入课程的学员不能开始答题
	r = postJSON(t, student2, "/api/practices/"+pid+"/proctor/start", map[string]any{})
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("未加入课程开始答题应 403，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 开始答题（幂等）
	var startResp struct {
		OK      bool           `json:"ok"`
		Attempt ProctorAttempt `json:"attempt"`
	}
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/proctor/start", map[string]any{
		"detail": map[string]any{"screen": "1920x1080", "ua": "test-agent"},
	}), 200, &startResp)
	if startResp.Attempt.StartedAt == "" || !startResp.OK {
		t.Fatalf("开始答题异常: %+v", startResp)
	}
	startedAt := startResp.Attempt.StartedAt
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/proctor/start", map[string]any{}), 200, &startResp)
	if startResp.Attempt.StartedAt != startedAt {
		t.Fatalf("重复开始应返回同一次开始时间: %q != %q", startResp.Attempt.StartedAt, startedAt)
	}

	// 学员详情里带出 strict 配置与答卷状态
	var detail struct {
		Practice  Practice           `json:"practice"`
		Attempt   *ProctorAttempt    `json:"attempt"`
		Questions []PracticeQuestion `json:"questions"`
	}
	getJSON(t, student, "/api/practices/"+pid, &detail)
	if detail.Practice.Proctor.Mode != "strict" || detail.Attempt == nil || detail.Attempt.StartedAt == "" {
		t.Fatalf("学员详情缺少监控状态: %+v", detail)
	}
	// 答题前不能看到标准答案
	if detail.Questions[0].Answer != "" {
		t.Fatal("严格模式答题前不应看到答案")
	}

	// 上报两次离开（带抓拍图），允许 1 次 → 第二次应超限
	img := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xFF}, 256))
	var evResp struct {
		Exits    int  `json:"exits"`
		MaxExits int  `json:"max_exits"`
		Over     bool `json:"over"`
	}
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/proctor/event", map[string]any{
		"kind": "leave", "detail": map[string]any{"reason": "visibility", "screen": "1920x1080"}, "image": img,
	}), 200, &evResp)
	if evResp.Exits != 1 || evResp.Over {
		t.Fatalf("第一次离开计数异常: %+v", evResp)
	}
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/proctor/event", map[string]any{
		"kind": "leave", "detail": map[string]any{"reason": "fullscreen"}, "image": img,
	}), 200, &evResp)
	if evResp.Exits != 2 || !evResp.Over {
		t.Fatalf("超限判定异常: %+v", evResp)
	}
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/proctor/event", map[string]any{
		"kind": "return",
	}), 200, &map[string]any{})

	// 老师监考台能看到离开次数、截图数、IP / 浏览器
	var overview struct {
		Proctor  ProctorConfig            `json:"proctor"`
		Students []ProctorStudentOverview `json:"students"`
	}
	getJSON(t, teacher, "/api/practices/"+pid+"/proctor/overview", &overview)
	if overview.Proctor.Mode != "strict" || len(overview.Students) != 1 {
		t.Fatalf("监考台汇总异常: %+v", overview)
	}
	stu := overview.Students[0]
	if stu.ExitCount != 2 || stu.SnapshotCount != 2 || stu.ProctorStatus != "violation" || !stu.Started {
		t.Fatalf("监考汇总异常: %+v", stu)
	}
	if stu.Screen != "1920x1080" || stu.UserAgent != "test-agent" {
		t.Fatalf("设备信息未记录: %+v", stu)
	}

	// 事件时间线 + 抓拍授权
	var events []ProctorEvent
	getJSON(t, teacher, "/api/practices/"+pid+"/proctor/events?student_id="+itoa(stuUser.ID), &events)
	if len(events) < 4 {
		t.Fatalf("事件时间线异常: %+v", events)
	}
	var snapID int64
	for _, ev := range events {
		if ev.Kind == "leave" && ev.ImageURL != "" && snapID == 0 {
			snapID = ev.ID
		}
	}
	if snapID == 0 {
		t.Fatal("离开事件缺少抓拍")
	}
	snapPath := "/api/proctor/snapshot/" + itoa(snapID)
	if code := getStatus(t, teacher, snapPath); code != 200 {
		t.Fatalf("老师查看抓拍应 200，实际 %d", code)
	}
	if code := getStatus(t, student, snapPath); code != 200 {
		t.Fatalf("本人查看抓拍应 200，实际 %d", code)
	}
	if code := getStatus(t, student2, snapPath); code != http.StatusForbidden {
		t.Fatalf("其他学员查看抓拍应 403，实际 %d", code)
	}

	// 交卷：服务端按 2 次离开 ≤ 上限 1 判定违规，但仍接收答卷
	var sub PracticeSubmission
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/submit", map[string]any{
		"answers": []map[string]any{{"question_id": detail.Questions[0].ID, "answer": "A"}},
	}), 201, &sub)
	if sub.Score != 5 || sub.Graded != true {
		t.Fatalf("客观题判分异常: %+v", sub)
	}
	getJSON(t, teacher, "/api/practices/"+pid+"/proctor/overview", &overview)
	if !overview.Students[0].Submitted || overview.Students[0].ProctorStatus != "violation" {
		t.Fatalf("交卷后监考状态异常: %+v", overview.Students[0])
	}
	var after []ProctorEvent
	getJSON(t, teacher, "/api/practices/"+pid+"/proctor/events?student_id="+itoa(stuUser.ID), &after)
	sawSubmit := false
	for _, ev := range after {
		if ev.Kind == "submit" {
			sawSubmit = true
		}
	}
	if !sawSubmit {
		t.Fatal("交卷事件没有落库")
	}
	// 已交卷后不能再上报事件
	r = postJSON(t, student, "/api/practices/"+pid+"/proctor/event", map[string]any{"kind": "leave"})
	if r.StatusCode != 400 {
		t.Fatalf("交卷后上报事件应 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 非本课老师不能看监考台
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "proctor_teacher2", "password": "teach-pass", "display_name": "别的老师", "role": "teacher"}), 200, nil)
	teacher2 := login(t, "proctor_teacher2", "teach-pass")
	if code := getStatus(t, teacher2, "/api/practices/"+pid+"/proctor/overview"); code != http.StatusForbidden {
		t.Fatalf("非本课老师查看监考台应 403，实际 %d", code)
	}
}

func TestVoiceFeedback(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "vf_teacher", "password": "teach-pass", "display_name": "语音老师", "role": "teacher"}), 200, nil)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "vf_stu", "password": "stu-pass", "display_name": "语音学员", "role": "student"}), 200, nil)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "vf_stu2", "password": "stu-pass", "display_name": "其他学员", "role": "student"}), 200, nil)
	teacher := login(t, "vf_teacher", "teach-pass")
	student := login(t, "vf_stu", "stu-pass")
	other := login(t, "vf_stu2", "stu-pass")

	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "语音评语课"}), 200, &course)
	decodeAs(t, postJSON(t, student, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)

	// 拍照 / 语音作业（无题目也能发布）
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "朗读作业", "kind": "homework", "subject": "english", "published": true,
		"allow_photo": true, "allow_voice": true,
	}), 201, &created)
	pid := itoa(created.ID)

	// 学员上传一段录音提交
	audio := bytes.Repeat([]byte{0x1A, 0x45, 0xDF, 0xA3}, 500)
	up := uploadAudioPart(t, student, "/api/practices/"+pid+"/files", audio, "audio/webm;codecs=opus")
	var att PracticeAttachment
	decodeAs(t, up, 200, &att)
	if att.Key == "" || att.Kind != "audio" {
		t.Fatalf("录音上传异常: %+v", att)
	}
	var sub PracticeSubmission
	decodeAs(t, postJSON(t, student, "/api/practices/"+pid+"/submit", map[string]any{
		"attachments": []PracticeAttachment{att},
	}), 201, &sub)
	if len(sub.Attachments) != 1 {
		t.Fatalf("提交附件异常: %+v", sub)
	}

	// 老师录语音评语并保存批改
	up = uploadAudioPart(t, teacher, "/api/practices/"+pid+"/submissions/"+itoa(sub.ID)+"/feedback-audio",
		bytes.Repeat([]byte{0x1A, 0x45, 0xDF, 0xA3}, 300), "audio/webm")
	var fb struct {
		Key string `json:"key"`
	}
	decodeAs(t, up, 200, &fb)
	if fb.Key == "" || !feedbackAudioKeyRe.MatchString(fb.Key) {
		t.Fatalf("语音评语上传异常: %+v", fb)
	}
	var graded PracticeSubmission
	decodeAs(t, postJSON(t, teacher, "/api/practices/"+pid+"/submissions/"+itoa(sub.ID)+"/grade", map[string]any{
		"score": 8, "comment": "读音不错", "feedback_audio": fb.Key, "feedback_audio_duration": 12.5,
	}), 200, &graded)
	if !graded.Graded || graded.FeedbackAudio != fb.Key || graded.FeedbackAudioDuration != 12.5 {
		t.Fatalf("语音评语未保存: %+v", graded)
	}

	// 学员出分后能看到 / 播放语音评语；其他学员不行
	var detail struct {
		Submission *PracticeSubmission `json:"submission"`
	}
	getJSON(t, student, "/api/practices/"+pid, &detail)
	if detail.Submission == nil || detail.Submission.FeedbackAudio != fb.Key {
		t.Fatalf("学员详情缺少语音评语: %+v", detail.Submission)
	}
	resp, err := student.Get(testBase + "/api/practices/" + pid + "/submissions/" + itoa(sub.ID) + "/feedback-audio")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) < 100 {
		t.Fatalf("学员播放语音评语异常: %d len=%d", resp.StatusCode, len(body))
	}
	if code := getStatus(t, other, "/api/practices/"+pid+"/submissions/"+itoa(sub.ID)+"/feedback-audio"); code != http.StatusForbidden {
		t.Fatalf("其他学员播放语音评语应 403，实际 %d", code)
	}

	// 伪造语音评语 key 被拒绝
	r := postJSON(t, teacher, "/api/practices/"+pid+"/submissions/"+itoa(sub.ID)+"/grade", map[string]any{
		"score": 8, "feedback_audio": "practice-feedback/1/2/" + strings.Repeat("a", 32) + ".webm",
	})
	if r.StatusCode != 400 {
		t.Fatalf("伪造语音评语 key 应 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()
}
