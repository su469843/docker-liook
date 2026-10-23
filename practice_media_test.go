package main

// 作业附件（拍照 / 语音）、老师圈画批注、内置题库：单测 + 端到端测试

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 1x1 PNG，用于模拟学员拍照上传
var testPNGBytes = func() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}()

func uploadWithKind(t *testing.T, c *http.Client, path, kind, filename string, content []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if kind != "" {
		if err := mw.WriteField("kind", kind); err != nil {
			t.Fatal(err)
		}
	}
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

func TestQuestionBankData(t *testing.T) {
	if len(questionBankSets) < 6 {
		t.Fatalf("内置题库应至少 6 套，实际 %d", len(questionBankSets))
	}
	subjects := map[string]bool{}
	for _, set := range questionBankSets {
		if set.ID == "" || set.Name == "" || set.Subject == "" || set.Desc == "" {
			t.Fatalf("题库集合信息不完整: %+v", set)
		}
		if len(set.Questions) < 5 {
			t.Fatalf("题库 %s 题目太少: %d", set.ID, len(set.Questions))
		}
		subjects[set.Subject] = true
		for i, q := range set.Questions {
			if strings.TrimSpace(q.Stem) == "" || q.Score <= 0 {
				t.Fatalf("题库 %s 第 %d 题题干/分值异常: %+v", set.ID, i+1, q)
			}
			switch q.Type {
			case "choice":
				if len(q.Options) < 2 || len(q.Options) > 8 {
					t.Fatalf("题库 %s 第 %d 题选项数量异常: %+v", set.ID, i+1, q.Options)
				}
				if len(q.Answer) != 1 || q.Answer[0] < 'A' || int(q.Answer[0]-'A') >= len(q.Options) {
					t.Fatalf("题库 %s 第 %d 题答案越界: %q", set.ID, i+1, q.Answer)
				}
				seen := map[string]bool{}
				for _, opt := range q.Options {
					if strings.TrimSpace(opt) == "" || seen[opt] {
						t.Fatalf("题库 %s 第 %d 题选项为空/重复: %+v", set.ID, i+1, q.Options)
					}
					seen[opt] = true
				}
			case "fill", "judge":
				if strings.TrimSpace(q.Answer) == "" {
					t.Fatalf("题库 %s 第 %d 题缺少答案", set.ID, i+1)
				}
			default:
				t.Fatalf("题库 %s 第 %d 题类型不支持: %s", set.ID, i+1, q.Type)
			}
		}
		// 题库里的题必须能通过服务端创建练习的校验
		reqs := make([]questionReq, 0, len(set.Questions))
		for _, q := range set.Questions {
			reqs = append(reqs, questionReq{Type: q.Type, Stem: q.Stem, Options: q.Options, Answer: q.Answer, Score: q.Score, Analysis: q.Analysis})
		}
		if _, msg := buildPracticeQuestions(reqs); msg != "" {
			t.Fatalf("题库 %s 的题目无法通过校验: %s", set.ID, msg)
		}
	}
	for _, subject := range []string{"english", "math", "chinese"} {
		if !subjects[subject] {
			t.Fatalf("题库缺少 %s 科目的题", subject)
		}
	}
}

func TestPracticeFileKey(t *testing.T) {
	key := "practice/12/34/0123456789abcdef0123456789abcdef.jpg"
	pid, uid, ext, ok := practiceFileKeyParts(key)
	if !ok || pid != 12 || uid != 34 || ext != ".jpg" {
		t.Fatalf("附件 key 解析异常: %d %d %q %v", pid, uid, ext, ok)
	}
	if practiceFileKind(key) != "image" {
		t.Fatal("jpg 应识别为图片")
	}
	audioKey := "practice/12/34/0123456789abcdef0123456789abcdef.webm"
	if practiceFileKind(audioKey) != "audio" {
		t.Fatal("webm 应识别为语音")
	}
	for _, bad := range []string{
		"practice/12/34/short.jpg",
		"practice/12/34/0123456789abcdef0123456789abcdef.exe",
		"chat/12/34/0123456789abcdef0123456789abcdef.jpg",
		"practice/0/34/0123456789abcdef0123456789abcdef.jpg",
	} {
		if _, _, _, ok := practiceFileKeyParts(bad); ok {
			t.Fatalf("非法 key 不应通过: %s", bad)
		}
	}
}

func TestNormalizeAnnotations(t *testing.T) {
	allowed := map[string]bool{"practice/1/2/x.jpg": true}
	raw := map[string][]AnnotationShape{
		"practice/1/2/x.jpg": {
			{Tool: "pen", Color: "#e03131", Width: 3, Points: []float64{1, 2, 3, 4}},
			{Tool: "circle", Color: "red", Width: 999, X: -5, Y: 10, W: 30, H: 30}, // 颜色/坐标/线宽都要被修正
			{Tool: "arrow", Color: "#1971c2", Width: 2, X: 1, Y: 1, X2: 100, Y2: 90},
			{Tool: "text", Color: "#111111", Width: 24, X: 5, Y: 6, Text: "评语"},
			{Tool: "text", Color: "#111111", Width: 24, X: 5, Y: 6, Text: "  "}, // 空文字丢弃
			{Tool: "hack", Color: "#000000", Width: 2},                          // 非法工具丢弃
		},
		"practice/9/9/other.jpg": {
			{Tool: "pen", Color: "#000000", Width: 2, Points: []float64{1, 2}},
		},
	}
	anns, msg := normalizeAnnotations(raw, allowed)
	if msg != "" {
		t.Fatalf("合法批注被拒绝: %s", msg)
	}
	if len(anns) != 1 {
		t.Fatalf("不属于本次提交的附件批注应被忽略: %+v", anns)
	}
	shapes := anns["practice/1/2/x.jpg"]
	if len(shapes) != 4 {
		t.Fatalf("批注图形数量异常: %+v", shapes)
	}
	if shapes[0].Tool != "pen" || len(shapes[0].Points) != 4 {
		t.Fatalf("画笔批注异常: %+v", shapes[0])
	}
	if shapes[3].Tool != "text" || shapes[3].Text != "评语" {
		t.Fatalf("文字批注异常: %+v", shapes[3])
	}
	if !annotationColorRe.MatchString(shapes[1].Color) {
		t.Fatalf("颜色应被修正为合法值: %+v", shapes[1])
	}
	// 超大批注要拒绝
	huge := map[string][]AnnotationShape{}
	pts := make([]float64, 9000)
	huge["practice/1/2/x.jpg"] = []AnnotationShape{{Tool: "pen", Color: "#000000", Width: 2, Points: pts}, {Tool: "pen", Color: "#000000", Width: 2, Points: []float64{1, 1}}}
	huge["practice/1/2/x.jpg"] = append(huge["practice/1/2/x.jpg"], make([]AnnotationShape, 0)...)
	if _, msg := normalizeAnnotations(huge, allowed); msg != "" {
		t.Fatalf("超大图形应被裁剪而不是报错: %s", msg)
	}
	tooMany := map[string][]AnnotationShape{}
	list := make([]AnnotationShape, 500)
	for i := range list {
		list[i] = AnnotationShape{Tool: "pen", Color: "#000000", Width: 2, Points: []float64{1, 1, 2, 2}}
	}
	tooMany["practice/1/2/x.jpg"] = list
	if _, msg := normalizeAnnotations(tooMany, allowed); msg == "" {
		t.Fatal("单图批注过多应被拒绝")
	}
}

func TestPracticeAttachmentFlow(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pm_teacher", "password": "teach-pass", "display_name": "李老师", "role": "teacher"}), 200, nil)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pm_student", "password": "stu-pass", "display_name": "小明", "role": "student"}), 200, nil)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pm_student2", "password": "stu-pass", "display_name": "小红", "role": "student"}), 200, nil)
	teacher := login(t, "pm_teacher", "teach-pass")
	student := login(t, "pm_student", "stu-pass")
	student2 := login(t, "pm_student2", "stu-pass")

	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "拍照作业课"}), 200, &course)
	decodeAs(t, postJSON(t, student, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)
	decodeAs(t, postJSON(t, student2, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)

	// 没有题目但允许拍照/语音 → 可以发布
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "周末作文拍照上传", "kind": "homework", "subject": "chinese", "published": true,
		"allow_photo": true, "allow_voice": true, "questions": []any{},
	}), 201, &created)
	if created.ID == 0 || created.QuestionCount != 0 || !created.AllowPhoto || !created.AllowVoice {
		t.Fatalf("拍照作业创建异常: %+v", created)
	}

	// 不允许拍照/语音时，空题目发布要被拒绝
	r := postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "空作业", "published": true, "allow_photo": false, "allow_voice": false,
	})
	if r.StatusCode != 400 {
		t.Fatalf("空题目且不允许附件应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 学员上传照片
	var att PracticeAttachment
	decodeAs(t, uploadWithKind(t, student, "/api/practices/"+itoa(created.ID)+"/files", "image", "homework.png", testPNGBytes), 200, &att)
	if att.Kind != "image" || !strings.HasPrefix(att.Key, "practice/") || att.Size <= 0 {
		t.Fatalf("照片上传异常: %+v", att)
	}
	if _, _, _, ok := practiceFileKeyParts(att.Key); !ok {
		t.Fatalf("上传返回的 key 不合法: %s", att.Key)
	}

	// 没有附件不能提交空作业
	r = postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{"answers": []any{}, "attachments": []any{}})
	if r.StatusCode != 400 {
		t.Fatalf("无附件不应能提交拍照作业，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 提交照片作业
	var sub PracticeSubmission
	decodeAs(t, postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{
		"answers":     []any{},
		"attachments": []map[string]any{{"key": att.Key, "kind": "image", "name": "homework.png"}},
	}), 201, &sub)
	if sub.Graded || len(sub.Attachments) != 1 || sub.Attachments[0].Key != att.Key {
		t.Fatalf("照片作业提交异常: %+v", sub)
	}

	// 已提交后不能再上传
	r = uploadWithKind(t, student, "/api/practices/"+itoa(created.ID)+"/files", "image", "again.png", testPNGBytes)
	if r.StatusCode != 400 {
		t.Fatalf("提交后上传应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 附件鉴权：本人和老师能看，其他学员不能看
	if code := getStatus(t, student, "/api/practice-files/"+att.Key); code != 200 {
		t.Fatalf("学员本人应能查看附件，实际 %d", code)
	}
	if code := getStatus(t, teacher, "/api/practice-files/"+att.Key); code != 200 {
		t.Fatalf("老师应能查看附件，实际 %d", code)
	}
	if code := getStatus(t, student2, "/api/practice-files/"+att.Key); code != 403 {
		t.Fatalf("其他学员不应能查看附件，实际 %d", code)
	}

	// 老师看提交：附件信息完整
	var subs []PracticeSubmission
	getJSON(t, teacher, "/api/practices/"+itoa(created.ID)+"/submissions", &subs)
	if len(subs) != 1 || len(subs[0].Attachments) != 1 {
		t.Fatalf("老师看提交附件异常: %+v", subs)
	}

	// 老师批改 + 圈画批注
	var graded PracticeSubmission
	decodeAs(t, postJSON(t, teacher, "/api/practices/"+itoa(created.ID)+"/submissions/"+itoa(subs[0].ID)+"/grade", map[string]any{
		"score": 88, "comment": "字要写工整，apple 读音是 ˈæpl",
		"annotations": map[string]any{
			att.Key: []map[string]any{
				{"tool": "circle", "color": "#e03131", "width": 3, "x": 10, "y": 20, "w": 100, "h": 60},
				{"tool": "text", "color": "#1971c2", "width": 24, "x": 120, "y": 30, "text": "拼写错了"},
			},
		},
	}), 200, &graded)
	if !graded.Graded || graded.Score != 88 || len(graded.Annotations[att.Key]) != 2 {
		t.Fatalf("批改+批注异常: %+v", graded)
	}

	// 学员批改后能看到批注
	var detail struct {
		Practice   Practice            `json:"practice"`
		Questions  []PracticeQuestion  `json:"questions"`
		Submission *PracticeSubmission `json:"submission"`
	}
	getJSON(t, student, "/api/practices/"+itoa(created.ID), &detail)
	if detail.Submission == nil || !detail.Submission.Graded || len(detail.Submission.Annotations[att.Key]) != 2 {
		t.Fatalf("学员批改后应看到批注: %+v", detail.Submission)
	}

	// 批注只允许针对本次提交的附件
	r = postJSON(t, teacher, "/api/practices/"+itoa(created.ID)+"/submissions/"+itoa(subs[0].ID)+"/grade", map[string]any{
		"score": 90, "annotations": map[string]any{
			"practice/999/999/0123456789abcdef0123456789abcdef.jpg": []map[string]any{
				{"tool": "pen", "color": "#e03131", "width": 3, "points": []float64{1, 2, 3, 4}},
			},
		},
	})
	if r.StatusCode != 200 {
		t.Fatalf("错误 key 的批注应被忽略，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 学员不能给其他学员的提交配批注（老师接口鉴权）
	if r := postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submissions/"+itoa(subs[0].ID)+"/grade", map[string]any{"score": 1}); r.StatusCode != 403 {
		t.Fatalf("学员不应能批改，实际 %d", r.StatusCode)
	}

	// 题库接口：老师可用，学员不可用
	var bank struct {
		Sets []QuestionBankSet `json:"sets"`
	}
	getJSON(t, teacher, "/api/question-bank", &bank)
	if len(bank.Sets) < 6 {
		t.Fatalf("题库接口返回异常: %d 套", len(bank.Sets))
	}
	if r := postJSON(t, student, "/api/question-bank", nil); r.StatusCode != 405 {
		// 题库只开放 GET；POST 由路由层直接 405
		r.Body.Close()
	}
	if code := getStatus(t, student, "/api/question-bank"); code != 403 {
		t.Fatalf("学员不应能访问题库，实际 %d", code)
	}

	// 老师把题库导入练习（模拟前端导入后创建）
	var bankPractice Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "题库导入·七年级单词", "kind": "word", "subject": "english", "published": true,
		"allow_photo": false, "allow_voice": false,
		"questions": []map[string]any{
			{"type": "choice", "stem": bank.Sets[0].Questions[0].Stem, "options": bank.Sets[0].Questions[0].Options,
				"answer": bank.Sets[0].Questions[0].Answer, "score": bank.Sets[0].Questions[0].Score},
		},
	}), 201, &bankPractice)
	if bankPractice.QuestionCount != 1 {
		t.Fatalf("题库导入练习创建异常: %+v", bankPractice)
	}

	// 老师列表里应带 allow_photo / allow_voice 信息
	var list []Practice
	getJSON(t, teacher, "/api/practices?course_id="+itoa(course.ID), &list)
	if len(list) != 2 {
		t.Fatalf("老师练习列表数量异常: %d", len(list))
	}
	var photoFlag bool
	for _, p := range list {
		if p.ID == created.ID {
			photoFlag = p.AllowPhoto && p.AllowVoice
		}
	}
	if !photoFlag {
		t.Fatalf("列表未返回附件开关: %+v", list)
	}
}

// 语音上传：用 webm 魔数文件走通接口（浏览器 MediaRecorder 默认就是 webm/opus）
func TestPracticeAudioUpload(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pa_teacher", "password": "teach-pass", "display_name": "王老师", "role": "teacher"}), 200, nil)
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pa_student", "password": "stu-pass", "display_name": "小刚", "role": "student"}), 200, nil)
	teacher := login(t, "pa_teacher", "teach-pass")
	student := login(t, "pa_student", "stu-pass")

	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "语音作业课"}), 200, &course)
	decodeAs(t, postJSON(t, student, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "朗读课文", "kind": "homework", "subject": "english", "published": true,
		"allow_photo": false, "allow_voice": true, "questions": []any{},
	}), 201, &created)

	// EBML 头（webm）足够让服务端 sniff 出音频；这里用 OggS 魔数确保 Go 能识别
	webm := append([]byte("OggS"), bytes.Repeat([]byte{0}, 64)...)
	// 先手动构造带 kind=audio 的上传
	var att PracticeAttachment
	decodeAs(t, uploadWithKind(t, student, "/api/practices/"+itoa(created.ID)+"/files", "audio", "voice.webm", webm), 200, &att)
	if att.Kind != "audio" || att.Size <= 0 {
		t.Fatalf("语音上传异常: %+v", att)
	}
	// 不允许多余类型：图片上传被拒绝
	r := uploadWithKind(t, student, "/api/practices/"+itoa(created.ID)+"/files", "image", "photo.png", testPNGBytes)
	if r.StatusCode != 400 {
		t.Fatalf("不允许拍照时上传图片应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	var sub PracticeSubmission
	decodeAs(t, postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{
		"attachments": []map[string]any{{"key": att.Key, "kind": "audio", "duration": 12.5}},
	}), 201, &sub)
	if len(sub.Attachments) != 1 || sub.Attachments[0].Kind != "audio" || sub.Attachments[0].Duration != 12.5 {
		t.Fatalf("语音提交异常: %+v", sub)
	}

	// 跨练习的 key 不能拿来提交
	r = postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{
		"attachments": []map[string]any{{"key": "practice/999/1/0123456789abcdef0123456789abcdef.jpg"}},
	})
	if r.StatusCode != 400 {
		t.Fatalf("跨练习附件应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()
}

func TestPracticeJSONBodyLimit(t *testing.T) {
	// 批注 JSON 有体积上限：超大 annotations 请求应返回 400（而不是把内存打爆）
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()
	admin := login(t, "admin", "admin-pass-123")
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pl_teacher", "password": "teach-pass", "display_name": "老师", "role": "teacher"}), 200, nil)
	teacher := login(t, "pl_teacher", "teach-pass")
	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "体积测试课"}), 200, &course)
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "题", "kind": "quiz", "published": true,
		"questions": []map[string]any{{"type": "fill", "stem": "1+1=", "answer": "2", "score": 1}},
	}), 201, &created)

	// 1MB 请求体上限：超长 description 直接被 MaxBytesReader 拒绝
	big := strings.Repeat("啊", 1_200_000)
	r := putJSON(t, teacher, "/api/practices/"+itoa(created.ID), map[string]any{"title": "x", "description": big})
	if r.StatusCode != 400 {
		t.Fatalf("超大 JSON 请求体应返回 400，实际 %d", r.StatusCode)
	}
	r.Body.Close()
}
