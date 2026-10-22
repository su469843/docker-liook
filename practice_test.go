package main

// 作业/练习/打卡：判分逻辑单测 + 端到端接口测试

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func decodeAs(t *testing.T, resp *http.Response, code int, v any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != code {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d（期望 %d）: %s", resp.StatusCode, code, b)
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

func TestAnswerCorrect(t *testing.T) {
	choice := &PracticeQuestion{Type: "choice", Options: []string{"苹果", "香蕉", "橙子"}, Answer: "A"}
	if !AnswerCorrect(choice, "a") || !AnswerCorrect(choice, "A. 苹果") || !AnswerCorrect(choice, "苹果") {
		t.Fatal("选择题答案比对失败")
	}
	if AnswerCorrect(choice, "B") || AnswerCorrect(choice, "") {
		t.Fatal("选择题错误答案被判对")
	}
	judge := &PracticeQuestion{Type: "judge", Answer: "true"}
	if !AnswerCorrect(judge, "对") || !AnswerCorrect(judge, "√") || !AnswerCorrect(judge, "true") {
		t.Fatal("判断题答案比对失败")
	}
	if AnswerCorrect(judge, "错") {
		t.Fatal("判断题错误答案被判对")
	}
	fill := &PracticeQuestion{Type: "fill", Answer: "apple|apples"}
	if !AnswerCorrect(fill, " Apple ") || !AnswerCorrect(fill, "apples") {
		t.Fatal("填空题答案比对失败")
	}
	if AnswerCorrect(fill, "appl") {
		t.Fatal("填空题错误答案被判对")
	}
	short := &PracticeQuestion{Type: "short", Answer: "任意"}
	if AnswerCorrect(short, "任意") {
		t.Fatal("简答题不应自动判分")
	}
}

func TestBuildPracticeQuestions(t *testing.T) {
	qs, msg := buildPracticeQuestions([]questionReq{
		{Type: "choice", Stem: "1+1=?", Options: []string{"1", "2"}, Answer: "B", Score: 5},
		{Type: "judge", Stem: "地球是圆的", Answer: "对", Score: 2},
		{Type: "fill", Stem: "apple 的中文", Answer: "苹果", Score: 3},
		{Type: "short", Stem: "用英语介绍自己", Score: 10},
	})
	if msg != "" || len(qs) != 4 {
		t.Fatalf("合法题目被拒绝: %s", msg)
	}
	if qs[0].Answer != "B" || qs[0].Options[1] != "2" {
		t.Fatalf("选择题答案归一化失败: %+v", qs[0])
	}
	if qs[1].Answer != "true" {
		t.Fatalf("判断题答案应归一化为 true，实际 %q", qs[1].Answer)
	}
	if _, msg = buildPracticeQuestions([]questionReq{{Type: "choice", Stem: "缺答案", Options: []string{"A", "B"}}}); msg == "" {
		t.Fatal("缺少答案的选择题应被拒绝")
	}
	if _, msg = buildPracticeQuestions([]questionReq{{Type: "fill", Stem: "缺答案"}}); msg == "" {
		t.Fatal("缺少答案的填空题应被拒绝")
	}
	if _, msg = buildPracticeQuestions([]questionReq{{Type: "choice", Stem: "选项不够", Options: []string{"A"}, Answer: "A"}}); msg == "" {
		t.Fatal("选项不足的选择题应被拒绝")
	}
}

func TestPracticeFlow(t *testing.T) {
	_, handler := newTestAPI(t)
	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")
	// 建老师 + 学员
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pt_teacher", "password": "teach-pass", "display_name": "李老师", "role": "teacher"}), 200, nil)
	var stu User
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pt_student", "password": "stu-pass", "display_name": "小测", "role": "student"}), 200, &stu)
	teacher := login(t, "pt_teacher", "teach-pass")
	student := login(t, "pt_student", "stu-pass")

	// 老师建课，学员用邀请码加入
	var course Course
	decodeAs(t, postJSON(t, teacher, "/api/courses", map[string]any{"title": "英语练习课"}), 200, &course)
	decodeAs(t, postJSON(t, student, "/api/join", map[string]string{"code": course.InviteCode}), 200, nil)

	// 老师布置一份含选择题/填空题/判断题/简答题的练习
	var created Practice
	decodeAs(t, postJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{
		"title": "Unit 1 单词小测", "kind": "quiz", "subject": "english", "published": true,
		"questions": []map[string]any{
			{"type": "choice", "stem": "“苹果”的英文是？", "options": []string{"apple", "banana", "orange"}, "answer": "A", "score": 5},
			{"type": "fill", "stem": "写出“书”的英文", "answer": "book", "score": 5},
			{"type": "judge", "stem": "cat 是“猫”", "answer": "对", "score": 2},
			{"type": "short", "stem": "用英语写一句问候语", "answer": "Hello!", "score": 8},
		},
	}), 201, &created)
	if created.ID == 0 || created.QuestionCount != 4 || created.TotalScore != 20 {
		t.Fatalf("练习创建异常: %+v", created)
	}

	// 学员：列表能看到、答案不可见
	var list []Practice
	getJSON(t, student, "/api/practices", &list)
	if len(list) != 1 || list[0].CanManage || list[0].Submission != nil {
		t.Fatalf("学员练习列表异常: %+v", list)
	}
	var detail struct {
		Practice   Practice            `json:"practice"`
		Questions  []PracticeQuestion  `json:"questions"`
		Submission *PracticeSubmission `json:"submission"`
	}
	getJSON(t, student, "/api/practices/"+itoa(created.ID), &detail)
	if len(detail.Questions) != 4 || detail.Questions[0].Answer != "" || detail.Submission != nil {
		t.Fatalf("提交前不应看到答案: %+v", detail.Questions[0])
	}

	// 学员提交（含简答题 → 待批改）
	var sub PracticeSubmission
	decodeAs(t, postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{
		"answers": []map[string]any{
			{"question_id": detail.Questions[0].ID, "answer": "A"},
			{"question_id": detail.Questions[1].ID, "answer": "Book"},
			{"question_id": detail.Questions[2].ID, "answer": "对"},
			{"question_id": detail.Questions[3].ID, "answer": "Hello"},
		},
	}), 201, &sub)
	if sub.Graded || sub.Score != 12 {
		t.Fatalf("客观题自动判分异常: %+v", sub)
	}

	// 重复提交被拒
	r := postJSON(t, student, "/api/practices/"+itoa(created.ID)+"/submit", map[string]any{"answers": []any{}})
	if r.StatusCode != 400 {
		t.Fatalf("重复提交应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 老师看提交、批改
	var subs []PracticeSubmission
	getJSON(t, teacher, "/api/practices/"+itoa(created.ID)+"/submissions", &subs)
	if len(subs) != 1 || subs[0].Graded || subs[0].StudentName != "小测" {
		t.Fatalf("老师提交列表异常: %+v", subs)
	}
	var graded PracticeSubmission
	decodeAs(t, postJSON(t, teacher, "/api/practices/"+itoa(created.ID)+"/submissions/"+itoa(subs[0].ID)+"/grade",
		map[string]any{"score": 18, "comment": "完成得不错"}), 200, &graded)
	if !graded.Graded || graded.Score != 18 || graded.Comment != "完成得不错" {
		t.Fatalf("批改异常: %+v", graded)
	}

	// 学员：批改后可见答案与成绩
	getJSON(t, student, "/api/practices/"+itoa(created.ID), &detail)
	if detail.Submission == nil || !detail.Submission.Graded || detail.Questions[0].Answer != "A" {
		t.Fatalf("批改后学员应能看到答案与成绩: %+v", detail)
	}

	// 已有提交后不允许改题目
	r = putJSON(t, teacher, "/api/practices/"+itoa(created.ID), map[string]any{
		"title": "改名可以", "questions": []map[string]any{{"type": "fill", "stem": "新题", "answer": "x"}},
	})
	if r.StatusCode != 400 {
		t.Fatalf("已有提交后改题目应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()

	// 打卡：英语单词 20 个
	var sum CheckinSummary
	decodeAs(t, postJSON(t, student, "/api/checkins", map[string]any{
		"course_id": course.ID, "subject": "english", "kind": "word", "count": 20, "minutes": 15, "content": "Unit 1 单词",
	}), 200, &sum)
	if !sum.TodayDone || sum.TodayCount != 20 || sum.Streak != 1 || sum.TotalDays != 1 {
		t.Fatalf("打卡汇总异常: %+v", sum)
	}
	// 同一科目同一天重复打卡是覆盖，不是重复计数
	decodeAs(t, postJSON(t, student, "/api/checkins", map[string]any{
		"course_id": course.ID, "subject": "english", "kind": "word", "count": 25,
	}), 200, &sum)
	if sum.TodayCount != 25 || sum.TotalDays != 1 || sum.MonthDays != 1 {
		t.Fatalf("重复打卡应覆盖当天数量: %+v", sum)
	}
	getJSON(t, student, "/api/checkins?month="+sum.Month, &sum)
	if sum.MonthDays != 1 || len(sum.Days) != 1 {
		t.Fatalf("按月查询打卡异常: %+v", sum)
	}
	var stats []StudentCheckinStat
	getJSON(t, teacher, "/api/courses/"+itoa(course.ID)+"/checkins?month="+sum.Month, &stats)
	if len(stats) != 1 || stats[0].Days != 1 || stats[0].Count != 25 || stats[0].StudentName != "小测" {
		t.Fatalf("老师看打卡统计异常: %+v", stats)
	}
	// 其他老师/学员不能看打卡统计
	if r := getStatus(t, student, "/api/courses/"+itoa(course.ID)+"/checkins"); r != 403 {
		t.Fatalf("学员不应能看打卡统计，实际 %d", r)
	}

	// 非本课老师不能布置作业
	decodeAs(t, postJSON(t, admin, "/api/users", map[string]any{
		"username": "pt_teacher2", "password": "teach-pass", "display_name": "赵老师", "role": "teacher"}), 200, nil)
	teacher2 := login(t, "pt_teacher2", "teach-pass")
	r = postJSON(t, teacher2, "/api/courses/"+itoa(course.ID)+"/practices", map[string]any{"title": "越权"})
	if r.StatusCode != 403 {
		t.Fatalf("非本课老师布置作业应被拒绝，实际 %d", r.StatusCode)
	}
	r.Body.Close()
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func getStatus(t *testing.T, c *http.Client, path string) int {
	t.Helper()
	resp, err := c.Get(testBase + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
