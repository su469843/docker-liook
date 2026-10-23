package main

// 作业 / 练习 / 打卡：数据模型与 SQLite 存取
//
// 设计要点：
//   - Practice 属于某门课程，老师出题、学员提交，题目支持 选择/填空/判断/简答 四种；
//   - 客观题提交时自动判分，含简答题时进入「待批改」，老师批改后出总分；
//   - Checkin 是独立的单词/句子打卡记录，不依赖题库（本期不导入内置题库）。

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// ---------------- 模型 ----------------

// Practice 一次作业、考卷练习或专项练习（单词/句子）
type Practice struct {
	ID          int64  `json:"id"`
	CourseID    int64  `json:"course_id"`
	CourseTitle string `json:"course_title,omitempty"`
	TeacherID   int64  `json:"teacher_id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`    // homework | quiz | word | sentence
	Subject     string `json:"subject"` // english | math | chinese | other
	Description string `json:"description"`
	Deadline    string `json:"deadline"` // RFC3339，空 = 不限
	Duration    int    `json:"duration"` // 建议用时（分钟），0 = 不限
	Published   bool   `json:"published"`
	AllowPhoto  bool   `json:"allow_photo"` // 允许拍照提交作业
	AllowVoice  bool   `json:"allow_voice"` // 允许语音提交作业
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	QuestionCount  int64               `json:"question_count"`
	TotalScore     float64             `json:"total_score"`
	SubmittedCount int64               `json:"submitted_count,omitempty"` // 老师视角
	PendingCount   int64               `json:"pending_count,omitempty"`   // 老师视角：待批改份数
	StudentCount   int64               `json:"student_count,omitempty"`   // 老师视角：课程学员数
	CanManage      bool                `json:"can_manage,omitempty"`
	Proctor        ProctorConfig       `json:"proctor"`              // 严格模式 / 监考配置
	Submission     *PracticeSubmission `json:"submission,omitempty"` // 学员视角：我的提交摘要
}

// PracticeQuestion 题目
type PracticeQuestion struct {
	ID         int64    `json:"id"`
	PracticeID int64    `json:"practice_id"`
	Position   int      `json:"position"`
	Type       string   `json:"type"` // choice | fill | judge | short
	Stem       string   `json:"stem"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer,omitempty"` // 标准答案（提交前不对学员返回）
	Score      float64  `json:"score"`
	Analysis   string   `json:"analysis,omitempty"`
}

// QuestionAnswer 学员某题的作答与判分结果
type QuestionAnswer struct {
	QuestionID int64   `json:"question_id"`
	Answer     string  `json:"answer"`
	Score      float64 `json:"score"`
	Correct    bool    `json:"correct"`
}

// PracticeSubmission 一次提交
type PracticeSubmission struct {
	ID          int64                `json:"id"`
	PracticeID  int64                `json:"practice_id"`
	StudentID   int64                `json:"student_id"`
	StudentName string               `json:"student_name,omitempty"`
	Answers     []QuestionAnswer     `json:"answers"`
	Score       float64              `json:"score"`
	Total       float64              `json:"total"`
	Status      string               `json:"status"` // submitted（待批改）| graded（已出分）
	Graded      bool                 `json:"graded"`
	Comment     string               `json:"comment"`
	Attachments []PracticeAttachment `json:"attachments"` // 拍照 / 语音附件
	// Annotations 老师圈画批注：附件 key -> 图形列表（未批改时对学员不可见）
	Annotations map[string][]AnnotationShape `json:"annotations,omitempty"`
	// FeedbackAudio 老师语音评语（存储 key，出分后学员可播放）
	FeedbackAudio         string  `json:"feedback_audio,omitempty"`
	FeedbackAudioDuration float64 `json:"feedback_audio_duration,omitempty"`
	SubmittedAt           string  `json:"submitted_at"`
	GradedAt              string  `json:"graded_at"`
}

// Checkin 单词/句子打卡（每人每天 + 类型 + 课程 唯一）
type Checkin struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	CourseID  int64  `json:"course_id"`
	Subject   string `json:"subject"` // english | math | chinese | other
	Kind      string `json:"kind"`    // word | sentence | reading
	Day       string `json:"day"`     // YYYY-MM-DD
	Count     int    `json:"count"`   // 单词/句子数量
	Minutes   int    `json:"minutes"` // 学习时长
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// CheckinSummary 我的打卡汇总（当前月 + 连续天数）
type CheckinSummary struct {
	Month      string    `json:"month"`
	Days       []Checkin `json:"days"`
	TodayDone  bool      `json:"today_done"`
	TodayCount int       `json:"today_count"`
	Streak     int       `json:"streak"`
	MonthDays  int       `json:"month_days"`
	MonthCount int       `json:"month_count"`
	TotalDays  int       `json:"total_days"`
	TotalCount int       `json:"total_count"`
}

// StudentCheckinStat 课程内单个学员的打卡统计（老师视角）
type StudentCheckinStat struct {
	StudentID   int64  `json:"student_id"`
	StudentName string `json:"student_name"`
	Days        int    `json:"days"`
	Count       int    `json:"count"`
	Today       bool   `json:"today"`
}

// ---------------- 建表 ----------------

func (s *Store) ensurePracticeSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS practices(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  teacher_id INTEGER NOT NULL DEFAULT 1,
  title TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'homework',
  subject TEXT NOT NULL DEFAULT 'english',
  description TEXT NOT NULL DEFAULT '',
  deadline TEXT NOT NULL DEFAULT '',
  duration INTEGER NOT NULL DEFAULT 0,
  published INTEGER NOT NULL DEFAULT 1,
  allow_photo INTEGER NOT NULL DEFAULT 1,
  allow_voice INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_practices_course ON practices(course_id);
CREATE TABLE IF NOT EXISTS practice_questions(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  practice_id INTEGER NOT NULL,
  position INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL DEFAULT 'choice',
  stem TEXT NOT NULL,
  options TEXT NOT NULL DEFAULT '[]',
  answer TEXT NOT NULL DEFAULT '',
  score REAL NOT NULL DEFAULT 1,
  analysis TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_questions_practice ON practice_questions(practice_id);
CREATE TABLE IF NOT EXISTS practice_submissions(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  practice_id INTEGER NOT NULL,
  student_id INTEGER NOT NULL,
  answers TEXT NOT NULL DEFAULT '[]',
  score REAL NOT NULL DEFAULT 0,
  total REAL NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'submitted',
  graded INTEGER NOT NULL DEFAULT 0,
  comment TEXT NOT NULL DEFAULT '',
  attachments TEXT NOT NULL DEFAULT '[]',
  annotations TEXT NOT NULL DEFAULT '{}',
  feedback_audio TEXT NOT NULL DEFAULT '',
  feedback_audio_duration REAL NOT NULL DEFAULT 0,
  submitted_at TEXT NOT NULL,
  graded_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_submissions_unique ON practice_submissions(practice_id, student_id);
CREATE TABLE IF NOT EXISTS checkins(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  course_id INTEGER NOT NULL DEFAULT 0,
  subject TEXT NOT NULL DEFAULT 'english',
  kind TEXT NOT NULL DEFAULT 'word',
  day TEXT NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  minutes INTEGER NOT NULL DEFAULT 0,
  content TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_checkins_unique ON checkins(user_id, day, kind, course_id);
CREATE INDEX IF NOT EXISTS idx_checkins_user_day ON checkins(user_id, day);
`)
	if err != nil {
		return err
	}
	return s.ensurePracticeMediaSchema()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------- 练习：读写 ----------------

const practiceBaseSelect = `
SELECT p.id, p.course_id, COALESCE(c.title, ''), p.teacher_id, p.title, p.kind, p.subject, p.description,
       p.deadline, p.duration, p.published, p.created_at, p.updated_at,
       (SELECT COUNT(*) FROM practice_questions q WHERE q.practice_id = p.id),
       (SELECT COALESCE(SUM(q.score), 0) FROM practice_questions q WHERE q.practice_id = p.id),
       (SELECT COUNT(*) FROM practice_submissions s WHERE s.practice_id = p.id),
       (SELECT COUNT(*) FROM practice_submissions s WHERE s.practice_id = p.id AND s.graded = 0),
       (SELECT COUNT(*) FROM enrollments en WHERE en.course_id = p.course_id),
       p.allow_photo, p.allow_voice`

type scanner interface {
	Scan(dest ...any) error
}

func scanPractice(sc scanner) (Practice, error) {
	var p Practice
	var published, allowPhoto, allowVoice int
	err := sc.Scan(&p.ID, &p.CourseID, &p.CourseTitle, &p.TeacherID, &p.Title, &p.Kind, &p.Subject,
		&p.Description, &p.Deadline, &p.Duration, &published, &p.CreatedAt, &p.UpdatedAt,
		&p.QuestionCount, &p.TotalScore, &p.SubmittedCount, &p.PendingCount, &p.StudentCount,
		&allowPhoto, &allowVoice)
	if err != nil {
		return p, err
	}
	p.Published = published == 1
	p.AllowPhoto = allowPhoto == 1
	p.AllowVoice = allowVoice == 1
	return p, nil
}

// scanPracticeWithSubmission 多 7 列：学员自己的提交摘要
func scanPracticeWithSubmission(sc scanner) (Practice, error) {
	var p Practice
	var published, allowPhoto, allowVoice int
	var subID sql.NullInt64
	var subStatus, subAt, subComment sql.NullString
	var subGraded sql.NullBool
	var subScore, subTotal sql.NullFloat64
	err := sc.Scan(&p.ID, &p.CourseID, &p.CourseTitle, &p.TeacherID, &p.Title, &p.Kind, &p.Subject,
		&p.Description, &p.Deadline, &p.Duration, &published, &p.CreatedAt, &p.UpdatedAt,
		&p.QuestionCount, &p.TotalScore, &p.SubmittedCount, &p.PendingCount, &p.StudentCount,
		&allowPhoto, &allowVoice,
		&subID, &subStatus, &subGraded, &subScore, &subTotal, &subAt, &subComment)
	if err != nil {
		return p, err
	}
	p.Published = published == 1
	p.AllowPhoto = allowPhoto == 1
	p.AllowVoice = allowVoice == 1
	if subID.Valid {
		p.Submission = &PracticeSubmission{
			ID: subID.Int64, PracticeID: p.ID, Status: subStatus.String, Graded: subGraded.Bool,
			Score: subScore.Float64, Total: subTotal.Float64, SubmittedAt: subAt.String, Comment: subComment.String,
		}
	}
	return p, nil
}

func scanPractices(rows *sql.Rows, withSubmission bool) ([]Practice, error) {
	defer rows.Close()
	out := make([]Practice, 0)
	for rows.Next() {
		var p Practice
		var err error
		if withSubmission {
			p, err = scanPracticeWithSubmission(rows)
		} else {
			p, err = scanPractice(rows)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func insertQuestions(tx *sql.Tx, practiceID int64, qs []PracticeQuestion) error {
	for i := range qs {
		opts, _ := json.Marshal(qs[i].Options)
		if _, err := tx.Exec(`INSERT INTO practice_questions(practice_id, position, type, stem, options, answer, score, analysis)
			VALUES(?,?,?,?,?,?,?,?)`,
			practiceID, i, qs[i].Type, qs[i].Stem, string(opts), qs[i].Answer, qs[i].Score, qs[i].Analysis); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreatePractice(p *Practice, qs []PracticeQuestion) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	res, err := tx.Exec(`INSERT INTO practices(course_id, teacher_id, title, kind, subject, description, deadline, duration, published, allow_photo, allow_voice, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.CourseID, p.TeacherID, p.Title, p.Kind, p.Subject, p.Description, p.Deadline, p.Duration,
		boolInt(p.Published), boolInt(p.AllowPhoto), boolInt(p.AllowVoice), t, t)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertQuestions(tx, id, qs); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdatePractice 更新练习。withQuestions=true 时替换全部题目（已有提交时调用方不应这么做）。
func (s *Store) UpdatePractice(p *Practice, qs []PracticeQuestion, withQuestions bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE practices SET title = ?, kind = ?, subject = ?, description = ?, deadline = ?,
		duration = ?, published = ?, allow_photo = ?, allow_voice = ?, updated_at = ? WHERE id = ?`,
		p.Title, p.Kind, p.Subject, p.Description, p.Deadline, p.Duration, boolInt(p.Published),
		boolInt(p.AllowPhoto), boolInt(p.AllowVoice), now(), p.ID); err != nil {
		return err
	}
	if withQuestions {
		if _, err := tx.Exec(`DELETE FROM practice_questions WHERE practice_id = ?`, p.ID); err != nil {
			return err
		}
		if err := insertQuestions(tx, p.ID, qs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeletePractice(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM practice_questions WHERE practice_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM practice_submissions WHERE practice_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM practices WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPractice(id int64) (*Practice, error) {
	p, err := scanPractice(s.db.QueryRow(practiceBaseSelect+` FROM practices p LEFT JOIN courses c ON c.id = p.course_id WHERE p.id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CountPracticeSubmissions(practiceID int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM practice_submissions WHERE practice_id = ?`, practiceID).Scan(&n)
	return n, err
}

func (s *Store) GetQuestions(practiceID int64) ([]PracticeQuestion, error) {
	rows, err := s.db.Query(`SELECT id, practice_id, position, type, stem, options, answer, score, analysis
		FROM practice_questions WHERE practice_id = ? ORDER BY position, id`, practiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PracticeQuestion, 0)
	for rows.Next() {
		var q PracticeQuestion
		var options string
		if err := rows.Scan(&q.ID, &q.PracticeID, &q.Position, &q.Type, &q.Stem, &options, &q.Answer, &q.Score, &q.Analysis); err != nil {
			return nil, err
		}
		if options != "" {
			_ = json.Unmarshal([]byte(options), &q.Options)
		}
		if q.Options == nil {
			q.Options = []string{}
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) ListPracticesByCourse(courseID int64, publishedOnly bool, studentID int64) ([]Practice, error) {
	if studentID > 0 {
		q := practiceBaseSelect + `,
			s.id, s.status, s.graded, s.score, s.total, s.submitted_at, s.comment
			FROM practices p
			LEFT JOIN courses c ON c.id = p.course_id
			LEFT JOIN practice_submissions s ON s.practice_id = p.id AND s.student_id = ?
			WHERE p.course_id = ?`
		args := []any{studentID, courseID}
		if publishedOnly {
			q += ` AND p.published = 1`
		}
		q += ` ORDER BY p.id DESC`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		return scanPractices(rows, true)
	}
	q := practiceBaseSelect + ` FROM practices p LEFT JOIN courses c ON c.id = p.course_id WHERE p.course_id = ?`
	if publishedOnly {
		q += ` AND p.published = 1`
	}
	q += ` ORDER BY p.id DESC`
	rows, err := s.db.Query(q, courseID)
	if err != nil {
		return nil, err
	}
	return scanPractices(rows, false)
}

func (s *Store) ListPracticesByTeacher(teacherID int64) ([]Practice, error) {
	rows, err := s.db.Query(practiceBaseSelect+` FROM practices p LEFT JOIN courses c ON c.id = p.course_id
		WHERE c.teacher_id = ? ORDER BY p.id DESC`, teacherID)
	if err != nil {
		return nil, err
	}
	return scanPractices(rows, false)
}

func (s *Store) ListPracticesAll() ([]Practice, error) {
	rows, err := s.db.Query(practiceBaseSelect + ` FROM practices p LEFT JOIN courses c ON c.id = p.course_id ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	return scanPractices(rows, false)
}

func (s *Store) ListPracticesForStudent(studentID int64) ([]Practice, error) {
	q := practiceBaseSelect + `,
		s.id, s.status, s.graded, s.score, s.total, s.submitted_at, s.comment
		FROM practices p
		JOIN courses c ON c.id = p.course_id
		JOIN enrollments e ON e.course_id = p.course_id AND e.student_id = ?
		LEFT JOIN practice_submissions s ON s.practice_id = p.id AND s.student_id = ?
		WHERE p.published = 1 ORDER BY p.id DESC`
	rows, err := s.db.Query(q, studentID, studentID)
	if err != nil {
		return nil, err
	}
	return scanPractices(rows, true)
}

// ---------------- 练习：提交与批改 ----------------

const submissionSelect = `SELECT id, practice_id, student_id, answers, score, total, status, graded, comment,
	attachments, annotations, feedback_audio, feedback_audio_duration, submitted_at, graded_at
	FROM practice_submissions`

func scanSubmission(sc scanner) (PracticeSubmission, error) {
	var sub PracticeSubmission
	var answers, attachments, annotations, feedbackAudio string
	var graded int
	err := sc.Scan(&sub.ID, &sub.PracticeID, &sub.StudentID, &answers, &sub.Score, &sub.Total,
		&sub.Status, &graded, &sub.Comment, &attachments, &annotations,
		&feedbackAudio, &sub.FeedbackAudioDuration, &sub.SubmittedAt, &sub.GradedAt)
	if err != nil {
		return sub, err
	}
	sub.Graded = graded == 1
	sub.FeedbackAudio = feedbackAudio
	if answers != "" {
		_ = json.Unmarshal([]byte(answers), &sub.Answers)
	}
	if sub.Answers == nil {
		sub.Answers = []QuestionAnswer{}
	}
	if attachments != "" {
		_ = json.Unmarshal([]byte(attachments), &sub.Attachments)
	}
	if sub.Attachments == nil {
		sub.Attachments = []PracticeAttachment{}
	}
	if annotations != "" && annotations != "{}" {
		_ = json.Unmarshal([]byte(annotations), &sub.Annotations)
	}
	return sub, nil
}

func (s *Store) GetPracticeSubmission(practiceID, studentID int64) (*PracticeSubmission, error) {
	sub, err := scanSubmission(s.db.QueryRow(submissionSelect+` WHERE practice_id = ? AND student_id = ?`, practiceID, studentID))
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) GetSubmissionByID(id int64) (*PracticeSubmission, error) {
	sub, err := scanSubmission(s.db.QueryRow(submissionSelect+` WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) CreateSubmission(sub *PracticeSubmission) (int64, error) {
	data, _ := json.Marshal(sub.Answers)
	atts, _ := json.Marshal(sub.Attachments)
	if sub.Attachments == nil {
		atts = []byte("[]")
	}
	anns := []byte("{}")
	if len(sub.Annotations) > 0 {
		anns, _ = json.Marshal(sub.Annotations)
	}
	res, err := s.db.Exec(`INSERT INTO practice_submissions(practice_id, student_id, answers, score, total, status, graded, comment,
		attachments, annotations, submitted_at, graded_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.PracticeID, sub.StudentID, string(data), sub.Score, sub.Total, sub.Status, boolInt(sub.Graded),
		sub.Comment, string(atts), string(anns), now(), "")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetSubmissionAnnotations 保存老师圈画批注（整体覆盖）
func (s *Store) SetSubmissionAnnotations(id int64, annotations map[string][]AnnotationShape) error {
	data := []byte("{}")
	if len(annotations) > 0 {
		if b, err := json.Marshal(annotations); err == nil {
			data = b
		}
	}
	_, err := s.db.Exec(`UPDATE practice_submissions SET annotations = ? WHERE id = ?`, string(data), id)
	return err
}

func (s *Store) GradeSubmission(id int64, score float64, comment, feedbackAudio string, feedbackDuration float64) error {
	_, err := s.db.Exec(`UPDATE practice_submissions SET score = ?, comment = ?, feedback_audio = ?,
		feedback_audio_duration = ?, status = 'graded', graded = 1, graded_at = ? WHERE id = ?`,
		score, comment, feedbackAudio, feedbackDuration, now(), id)
	return err
}

func (s *Store) ListSubmissions(practiceID int64) ([]PracticeSubmission, error) {
	rows, err := s.db.Query(`SELECT s.id, s.practice_id, s.student_id, s.answers, s.score, s.total, s.status, s.graded,
		s.comment, s.attachments, s.annotations, s.feedback_audio, s.feedback_audio_duration,
		s.submitted_at, s.graded_at, COALESCE(u.display_name, '')
		FROM practice_submissions s LEFT JOIN users u ON u.id = s.student_id
		WHERE s.practice_id = ? ORDER BY s.graded ASC, s.id DESC`, practiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PracticeSubmission, 0)
	for rows.Next() {
		var sub PracticeSubmission
		var answers, attachments, annotations, feedbackAudio string
		var graded int
		if err := rows.Scan(&sub.ID, &sub.PracticeID, &sub.StudentID, &answers, &sub.Score, &sub.Total,
			&sub.Status, &graded, &sub.Comment, &attachments, &annotations,
			&feedbackAudio, &sub.FeedbackAudioDuration, &sub.SubmittedAt, &sub.GradedAt, &sub.StudentName); err != nil {
			return nil, err
		}
		sub.Graded = graded == 1
		sub.FeedbackAudio = feedbackAudio
		if answers != "" {
			_ = json.Unmarshal([]byte(answers), &sub.Answers)
		}
		if sub.Answers == nil {
			sub.Answers = []QuestionAnswer{}
		}
		if attachments != "" {
			_ = json.Unmarshal([]byte(attachments), &sub.Attachments)
		}
		if sub.Attachments == nil {
			sub.Attachments = []PracticeAttachment{}
		}
		if annotations != "" && annotations != "{}" {
			_ = json.Unmarshal([]byte(annotations), &sub.Annotations)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ---------------- 自动判分 ----------------

// normalizeAnswer 归一化：去首尾空白、全角空格、连续空白，英文不区分大小写
func normalizeAnswer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\u3000", " ")
	return strings.Join(strings.Fields(s), " ")
}

func judgeValue(s string) string {
	switch normalizeAnswer(s) {
	case "true", "t", "yes", "y", "1", "对", "√", "正确":
		return "true"
	case "false", "f", "no", "n", "0", "错", "×", "x", "错误":
		return "false"
	}
	return ""
}

// choiceAnswerHasSeparator 判断「选项字母 + 分隔符」形式：如 "A. 内容"、"A、内容"、"A 内容"。
// 注意字符串下标取到的是字节，中文「、」是多字节，只能用 HasPrefix 判断。
func choiceAnswerHasSeparator(s string) bool {
	if len(s) < 2 {
		return false
	}
	rest := s[1:]
	return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "、") || strings.HasPrefix(rest, " ")
}

// AnswerCorrect 客观题判分（填空支持用 | 分隔多个可接受答案）
func AnswerCorrect(q *PracticeQuestion, ans string) bool {
	if q == nil {
		return false
	}
	switch q.Type {
	case "choice":
		a := strings.ToUpper(strings.TrimSpace(ans))
		e := strings.ToUpper(strings.TrimSpace(q.Answer))
		if a == "" || e == "" {
			return false
		}
		if a == e {
			return true
		}
		// 答案存字母时：兼容「A. 选项内容」「A、选项内容」作答，或直接写选项内容
		if len(e) == 1 && e[0] >= 'A' && e[0] <= 'H' {
			if choiceAnswerHasSeparator(a) {
				return a[0] == e[0]
			}
			idx := int(e[0] - 'A')
			if idx < len(q.Options) && normalizeAnswer(ans) == normalizeAnswer(q.Options[idx]) {
				return true
			}
			return false
		}
		// 答案存的是选项内容：也接受写字母
		if len(a) == 1 && a[0] >= 'A' && a[0] <= 'H' {
			idx := int(a[0] - 'A')
			if idx < len(q.Options) && normalizeAnswer(q.Options[idx]) == normalizeAnswer(q.Answer) {
				return true
			}
		}
		return false
	case "judge":
		v := judgeValue(ans)
		return v != "" && v == judgeValue(q.Answer)
	case "fill":
		na := normalizeAnswer(ans)
		if na == "" {
			return false
		}
		for _, alt := range strings.Split(q.Answer, "|") {
			if alt = normalizeAnswer(alt); alt != "" && na == alt {
				return true
			}
		}
		return false
	}
	return false
}

// ---------------- 打卡 ----------------

// UpsertCheckin 同一天同类型同课程重复打卡时覆盖数量/时长/内容
func (s *Store) UpsertCheckin(c *Checkin) error {
	_, err := s.db.Exec(`INSERT INTO checkins(user_id, course_id, subject, kind, day, count, minutes, content, created_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, day, kind, course_id) DO UPDATE SET
			subject = excluded.subject, count = excluded.count,
			minutes = excluded.minutes, content = excluded.content`,
		c.UserID, c.CourseID, c.Subject, c.Kind, c.Day, c.Count, c.Minutes, c.Content, now())
	return err
}

func (s *Store) ListCheckinsBetween(userID int64, from, to string) ([]Checkin, error) {
	rows, err := s.db.Query(`SELECT id, user_id, course_id, subject, kind, day, count, minutes, content, created_at
		FROM checkins WHERE user_id = ? AND day >= ? AND day < ? ORDER BY day, id`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Checkin, 0)
	for rows.Next() {
		var c Checkin
		if err := rows.Scan(&c.ID, &c.UserID, &c.CourseID, &c.Subject, &c.Kind, &c.Day, &c.Count, &c.Minutes, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func parseMonth(month string) (from, to, label string, ok bool) {
	month = strings.TrimSpace(month)
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", "", false
	}
	return t.Format("2006-01-02"), t.AddDate(0, 1, 0).Format("2006-01-02"), t.Format("2006-01"), true
}

func (s *Store) CheckinSummary(userID int64, month string) (*CheckinSummary, error) {
	from, to, label, ok := parseMonth(month)
	if !ok {
		return nil, sql.ErrNoRows
	}
	sum := &CheckinSummary{Month: label, Days: []Checkin{}}
	days, err := s.ListCheckinsBetween(userID, from, to)
	if err != nil {
		return nil, err
	}
	sum.Days = days
	today := time.Now().Format("2006-01-02")
	daySet := map[string]bool{}
	for _, c := range days {
		daySet[c.Day] = true
		if c.Day == today {
			sum.TodayDone = true
			sum.TodayCount += c.Count
		}
	}
	sum.MonthDays = len(daySet)
	for _, c := range days {
		sum.MonthCount += c.Count
	}
	// 连续打卡：从今天（今天没打则从昨天）往前数
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT day), COALESCE(SUM(count), 0) FROM checkins WHERE user_id = ?`, userID).
		Scan(&sum.TotalDays, &sum.TotalCount); err != nil {
		return nil, err
	}
	allDays := map[string]bool{}
	rows, err := s.db.Query(`SELECT DISTINCT day FROM checkins WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		allDays[d] = true
	}
	rows.Close()
	cur := time.Now()
	if !allDays[cur.Format("2006-01-02")] {
		cur = cur.AddDate(0, 0, -1)
	}
	for allDays[cur.Format("2006-01-02")] {
		sum.Streak++
		cur = cur.AddDate(0, 0, -1)
	}
	return sum, nil
}

func (s *Store) CourseCheckinStats(courseID int64, month string) ([]StudentCheckinStat, error) {
	from, to, _, ok := parseMonth(month)
	if !ok {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.Query(`SELECT u.id, u.display_name,
		COUNT(DISTINCT c.day), COALESCE(SUM(c.count), 0),
		COALESCE(MAX(CASE WHEN c.day = ? THEN 1 ELSE 0 END), 0)
		FROM enrollments e
		JOIN users u ON u.id = e.student_id
		LEFT JOIN checkins c ON c.user_id = u.id AND c.course_id = ? AND c.day >= ? AND c.day < ?
		WHERE e.course_id = ?
		GROUP BY u.id ORDER BY COUNT(DISTINCT c.day) DESC, u.id`, time.Now().Format("2006-01-02"), courseID, from, to, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StudentCheckinStat, 0)
	for rows.Next() {
		var st StudentCheckinStat
		var today int
		if err := rows.Scan(&st.StudentID, &st.StudentName, &st.Days, &st.Count, &today); err != nil {
			return nil, err
		}
		st.Today = today == 1
		out = append(out, st)
	}
	return out, rows.Err()
}
