package main

// 作业 / 练习 / 打卡：HTTP 接口

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var practiceKinds = map[string]bool{"homework": true, "quiz": true, "word": true, "sentence": true}
var practiceSubjects = map[string]bool{"english": true, "math": true, "chinese": true, "other": true}
var checkinKinds = map[string]bool{"word": true, "sentence": true, "reading": true}

// trimText 去空白并按字符数截断（防止超长内容塞爆 1MB 请求体和页面）
func trimText(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		s = string(r[:max])
	}
	return s
}

func normalizeDeadline(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Format(time.RFC3339), true
		}
	}
	return "", false
}

// normalizeChoiceAnswer 选择题答案统一成 A-H 字母（也兼容填写选项原文）
func normalizeChoiceAnswer(raw string, options []string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	up := strings.ToUpper(raw)
	if len(up) == 1 && up[0] >= 'A' && up[0] <= 'H' {
		return up
	}
	if len(up) > 1 && up[0] >= 'A' && up[0] <= 'H' && choiceAnswerHasSeparator(up) {
		return string(up[0])
	}
	for i, opt := range options {
		if normalizeAnswer(opt) == normalizeAnswer(raw) {
			return string(rune('A' + i))
		}
	}
	return raw
}

// ---------------- 请求体 ----------------

type questionReq struct {
	Type     string   `json:"type"`
	Stem     string   `json:"stem"`
	Options  []string `json:"options"`
	Answer   string   `json:"answer"`
	Score    float64  `json:"score"`
	Analysis string   `json:"analysis"`
}

type practiceReq struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	Deadline    string         `json:"deadline"`
	Duration    int            `json:"duration"`
	Published   *bool          `json:"published"`
	Questions   *[]questionReq `json:"questions"` // nil = 更新时不动题目
}

func decodePracticeReq(r *http.Request) (*practiceReq, error) {
	var req practiceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

// applyPracticeReq 校验并写入练习基本信息
func applyPracticeReq(p *Practice, req *practiceReq) string {
	if title := trimTitle(req.Title); title != "" {
		p.Title = title
	} else if p.ID == 0 {
		p.Title = "未命名练习"
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		if p.ID != 0 {
			kind = p.Kind // 只改发布状态等局部字段时，保留原类型
		} else {
			kind = "homework"
		}
	}
	if !practiceKinds[kind] {
		return "练习类型无效"
	}
	p.Kind = kind
	subject := strings.ToLower(strings.TrimSpace(req.Subject))
	if subject == "" {
		if p.ID != 0 {
			subject = p.Subject
		} else {
			subject = "english"
		}
	}
	if !practiceSubjects[subject] {
		return "科目无效"
	}
	p.Subject = subject
	p.Description = trimText(req.Description, 5000)
	deadline, ok := normalizeDeadline(req.Deadline)
	if !ok {
		return "截止时间格式无效"
	}
	p.Deadline = deadline
	duration := req.Duration
	if duration < 0 {
		duration = 0
	}
	if duration > 1440 {
		duration = 1440
	}
	p.Duration = duration
	return ""
}

func buildPracticeQuestions(reqs []questionReq) ([]PracticeQuestion, string) {
	if len(reqs) > 200 {
		return nil, "单份练习最多 200 道题"
	}
	out := make([]PracticeQuestion, 0, len(reqs))
	for i, rq := range reqs {
		no := i + 1
		typ := strings.ToLower(strings.TrimSpace(rq.Type))
		switch typ {
		case "choice", "fill", "judge", "short":
		default:
			return nil, fmt.Sprintf("第 %d 题类型无效", no)
		}
		stem := trimText(rq.Stem, 4000)
		if stem == "" {
			return nil, fmt.Sprintf("第 %d 题题干不能为空", no)
		}
		score := rq.Score
		if score <= 0 {
			score = 1
		}
		if score > 100 {
			return nil, fmt.Sprintf("第 %d 题分值不能超过 100", no)
		}
		answer := strings.TrimSpace(rq.Answer)
		options := []string{}
		switch typ {
		case "choice":
			for _, o := range rq.Options {
				if o = strings.TrimSpace(o); o != "" {
					options = append(options, trimText(o, 500))
				}
			}
			if len(options) < 2 {
				return nil, fmt.Sprintf("第 %d 题至少需要 2 个选项", no)
			}
			if len(options) > 8 {
				return nil, fmt.Sprintf("第 %d 题最多 8 个选项", no)
			}
			answer = normalizeChoiceAnswer(answer, options)
			if answer == "" {
				return nil, fmt.Sprintf("第 %d 题需要设置正确答案", no)
			}
		case "judge":
			v := judgeValue(answer)
			if v == "" {
				return nil, fmt.Sprintf("第 %d 题答案只能是 对 / 错", no)
			}
			answer = v
		case "fill":
			if answer == "" {
				return nil, fmt.Sprintf("第 %d 题需要设置参考答案（多个答案用 | 分隔）", no)
			}
		}
		out = append(out, PracticeQuestion{
			Type: typ, Stem: stem, Options: options, Answer: answer,
			Score: score, Analysis: trimText(rq.Analysis, 4000),
		})
	}
	return out, ""
}

// ---------------- 练习：接口 ----------------

func (a *API) handleListPractices(w http.ResponseWriter, r *http.Request, u *User) {
	q := r.URL.Query()
	var list []Practice
	var err error
	if cidStr := strings.TrimSpace(q.Get("course_id")); cidStr != "" {
		cid, parseErr := strconv.ParseInt(cidStr, 10, 64)
		if parseErr != nil || cid <= 0 {
			writeErr(w, 400, "无效的课程 ID")
			return
		}
		course, ok := a.canAccessCourse(u, cid)
		if !ok {
			writeErr(w, http.StatusForbidden, "无权查看该课程的练习")
			return
		}
		manage := a.canManage(course, u)
		if u.Role == "student" {
			list, err = a.db.ListPracticesByCourse(cid, true, u.ID)
		} else {
			list, err = a.db.ListPracticesByCourse(cid, !manage, 0)
		}
		for i := range list {
			list[i].CanManage = manage
		}
	} else {
		switch u.Role {
		case "admin":
			list, err = a.db.ListPracticesAll()
			for i := range list {
				list[i].CanManage = true
			}
		case "teacher":
			list, err = a.db.ListPracticesByTeacher(u.ID)
			for i := range list {
				list[i].CanManage = true
			}
		default:
			list, err = a.db.ListPracticesForStudent(u.ID)
		}
	}
	if err != nil {
		writeErr(w, 500, "查询练习失败: "+err.Error())
		return
	}
	if list == nil {
		list = []Practice{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) handleGetPractice(w http.ResponseWriter, r *http.Request, u *User) {
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
	course, err := a.db.GetCourse(p.CourseID)
	if err != nil {
		writeErr(w, 404, "练习所属课程不存在")
		return
	}
	manage := a.canManage(course, u)
	if manage {
		p.CanManage = true
	} else {
		enrolled, _ := a.db.IsEnrolled(course.ID, u.ID)
		if !enrolled || !p.Published {
			writeErr(w, http.StatusForbidden, "无权查看该练习")
			return
		}
	}
	questions, err := a.db.GetQuestions(id)
	if err != nil {
		writeErr(w, 500, "查询题目失败: "+err.Error())
		return
	}
	var sub *PracticeSubmission
	if !manage {
		if got, e := a.db.GetPracticeSubmission(id, u.ID); e == nil {
			sub = got
		}
	}
	// 标准答案/解析：老师始终可见；学员提交且已出分后可见
	reveal := manage || (sub != nil && sub.Graded)
	if !reveal {
		for i := range questions {
			questions[i].Answer = ""
			questions[i].Analysis = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"practice":   p,
		"questions":  questions,
		"submission": sub,
	})
}

func (a *API) handleCreatePractice(w http.ResponseWriter, r *http.Request, u *User) {
	courseID, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	if _, ok := a.canManageCourse(u, courseID); !ok {
		writeErr(w, http.StatusForbidden, "无权向该课程布置作业")
		return
	}
	req, err := decodePracticeReq(r)
	if err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	p := &Practice{CourseID: courseID, TeacherID: u.ID}
	if msg := applyPracticeReq(p, req); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	var qs []PracticeQuestion
	if req.Questions != nil {
		var msg string
		if qs, msg = buildPracticeQuestions(*req.Questions); msg != "" {
			writeErr(w, 400, msg)
			return
		}
	}
	// 默认：有题目就直接发布，方便老师立刻在学员端看到效果
	p.Published = len(qs) > 0
	if req.Published != nil {
		p.Published = *req.Published
	}
	if p.Published && len(qs) == 0 {
		writeErr(w, 400, "还没有题目，请先添加题目再发布")
		return
	}
	id, err := a.db.CreatePractice(p, qs)
	if err != nil {
		writeErr(w, 500, "创建练习失败: "+err.Error())
		return
	}
	saved, err := a.db.GetPractice(id)
	if err != nil {
		writeErr(w, 500, "练习回查失败")
		return
	}
	saved.CanManage = true
	writeJSON(w, http.StatusCreated, saved)
}

func (a *API) handleUpdatePractice(w http.ResponseWriter, r *http.Request, u *User) {
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
		writeErr(w, http.StatusForbidden, "无权修改该练习")
		return
	}
	req, err := decodePracticeReq(r)
	if err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if msg := applyPracticeReq(p, req); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	withQuestions := false
	var qs []PracticeQuestion
	if req.Questions != nil {
		subs, _ := a.db.CountPracticeSubmissions(id)
		if subs > 0 {
			writeErr(w, 400, "已有学员提交，题目不能再修改（标题、说明、截止时间仍可改）")
			return
		}
		var msg string
		if qs, msg = buildPracticeQuestions(*req.Questions); msg != "" {
			writeErr(w, 400, msg)
			return
		}
		withQuestions = true
	}
	published := p.Published
	if req.Published != nil {
		published = *req.Published
	}
	totalQuestions := p.QuestionCount
	if withQuestions {
		totalQuestions = int64(len(qs))
	}
	if published && totalQuestions == 0 {
		writeErr(w, 400, "还没有题目，请先添加题目再发布")
		return
	}
	p.Published = published
	if err := a.db.UpdatePractice(p, qs, withQuestions); err != nil {
		writeErr(w, 500, "保存练习失败: "+err.Error())
		return
	}
	saved, err := a.db.GetPractice(id)
	if err != nil {
		writeErr(w, 500, "练习回查失败")
		return
	}
	saved.CanManage = true
	writeJSON(w, http.StatusOK, saved)
}

func (a *API) handleDeletePractice(w http.ResponseWriter, r *http.Request, u *User) {
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
		writeErr(w, http.StatusForbidden, "无权删除该练习")
		return
	}
	if err := a.db.DeletePractice(id); err != nil {
		writeErr(w, 500, "删除练习失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSubmitPractice 学员提交作答：客观题立即判分，含简答题则等老师批改
func (a *API) handleSubmitPractice(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "student" {
		writeErr(w, http.StatusForbidden, "只有学员可以提交练习")
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
			writeErr(w, 400, "已过截止时间，不能再提交")
			return
		}
	}
	if existing, e := a.db.GetPracticeSubmission(id, u.ID); e == nil && existing != nil {
		writeErr(w, 400, "你已提交过这份练习，不能重复提交")
		return
	}
	questions, err := a.db.GetQuestions(id)
	if err != nil {
		writeErr(w, 500, "查询题目失败")
		return
	}
	if len(questions) == 0 {
		writeErr(w, 400, "这份练习还没有题目")
		return
	}
	var req struct {
		Answers []QuestionAnswer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	answers := make(map[int64]string, len(req.Answers))
	for _, ans := range req.Answers {
		answers[ans.QuestionID] = trimText(ans.Answer, 4000)
	}
	graded := true
	var total, score float64
	results := make([]QuestionAnswer, 0, len(questions))
	for i := range questions {
		q := &questions[i]
		total += q.Score
		qa := QuestionAnswer{QuestionID: q.ID, Answer: answers[q.ID]}
		if q.Type == "short" {
			graded = false
			qa.Correct = false
			qa.Score = 0
		} else if AnswerCorrect(q, qa.Answer) {
			qa.Correct = true
			qa.Score = q.Score
			score += q.Score
		}
		results = append(results, qa)
	}
	status := "submitted"
	if graded {
		status = "graded"
	}
	sub := &PracticeSubmission{
		PracticeID: id, StudentID: u.ID, Answers: results,
		Score: score, Total: total, Status: status, Graded: graded,
	}
	sid, err := a.db.CreateSubmission(sub)
	if err != nil {
		writeErr(w, 500, "提交失败: "+err.Error())
		return
	}
	saved, err := a.db.GetSubmissionByID(sid)
	if err != nil {
		writeErr(w, 500, "提交回查失败")
		return
	}
	saved.StudentName = u.DisplayName
	writeJSON(w, http.StatusCreated, saved)
}

func (a *API) handleListPracticeSubmissions(w http.ResponseWriter, r *http.Request, u *User) {
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
		writeErr(w, http.StatusForbidden, "无权查看该练习的提交")
		return
	}
	subs, err := a.db.ListSubmissions(id)
	if err != nil {
		writeErr(w, 500, "查询提交失败: "+err.Error())
		return
	}
	if subs == nil {
		subs = []PracticeSubmission{}
	}
	writeJSON(w, http.StatusOK, subs)
}

func (a *API) handleGradePracticeSubmission(w http.ResponseWriter, r *http.Request, u *User) {
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
	total := sub.Total
	if total <= 0 {
		if qs, e := a.db.GetQuestions(id); e == nil {
			for _, q := range qs {
				total += q.Score
			}
		}
	}
	var req struct {
		Score   *float64 `json:"score"`
		Comment string   `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	score := sub.Score
	if req.Score != nil {
		score = *req.Score
	}
	if score < 0 {
		score = 0
	}
	if total > 0 && score > total {
		score = total
	}
	comment := trimText(req.Comment, 2000)
	if err := a.db.GradeSubmission(sid, score, comment); err != nil {
		writeErr(w, 500, "批改失败: "+err.Error())
		return
	}
	saved, err := a.db.GetSubmissionByID(sid)
	if err != nil {
		writeErr(w, 500, "提交回查失败")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// ---------------- 打卡接口 ----------------

func (a *API) handleListCheckins(w http.ResponseWriter, r *http.Request, u *User) {
	sum, err := a.db.CheckinSummary(u.ID, r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, 400, "月份格式无效")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (a *API) handleCreateCheckin(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		CourseID int64  `json:"course_id"`
		Subject  string `json:"subject"`
		Kind     string `json:"kind"`
		Count    int    `json:"count"`
		Minutes  int    `json:"minutes"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	subject := strings.ToLower(strings.TrimSpace(req.Subject))
	if subject == "" {
		subject = "english"
	}
	if !practiceSubjects[subject] {
		writeErr(w, 400, "科目无效")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "word"
	}
	if !checkinKinds[kind] {
		writeErr(w, 400, "打卡类型无效")
		return
	}
	if req.CourseID < 0 {
		req.CourseID = 0
	}
	if req.CourseID > 0 {
		if _, ok := a.canAccessCourse(u, req.CourseID); !ok {
			writeErr(w, http.StatusForbidden, "无权关联该课程")
			return
		}
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 2000 {
		count = 2000
	}
	minutes := req.Minutes
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 1440 {
		minutes = 1440
	}
	c := &Checkin{
		UserID: u.ID, CourseID: req.CourseID, Subject: subject, Kind: kind,
		Day: time.Now().Format("2006-01-02"), Count: count, Minutes: minutes,
		Content: trimText(req.Content, 2000),
	}
	if err := a.db.UpsertCheckin(c); err != nil {
		writeErr(w, 500, "打卡失败: "+err.Error())
		return
	}
	sum, err := a.db.CheckinSummary(u.ID, c.Day[:7])
	if err != nil {
		writeErr(w, 500, "统计失败")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (a *API) handleCourseCheckins(w http.ResponseWriter, r *http.Request, u *User) {
	courseID, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效的课程 ID")
		return
	}
	course, ok := a.canAccessCourse(u, courseID)
	if !ok {
		writeErr(w, http.StatusForbidden, "无权查看该课程")
		return
	}
	if !a.canManage(course, u) {
		writeErr(w, http.StatusForbidden, "只有老师可以查看打卡统计")
		return
	}
	stats, err := a.db.CourseCheckinStats(courseID, r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, 400, "月份格式无效")
		return
	}
	if stats == nil {
		stats = []StudentCheckinStat{}
	}
	writeJSON(w, http.StatusOK, stats)
}
