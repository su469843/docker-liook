package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// ---------------- 模型 ----------------

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`   // admin | teacher | student
	Status       string `json:"status"` // active | disabled
	CreatedAt    string `json:"created_at"`
}

type Course struct {
	ID          int64  `json:"id"`
	TeacherID   int64  `json:"teacher_id"`
	TeacherName string `json:"teacher_name,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	InviteCode  string `json:"invite_code,omitempty"`
	CreatedAt   string `json:"created_at"`
	VideoCount  int64  `json:"video_count"`
	StudentN    int64  `json:"student_count"`
}

type Video struct {
	ID        int64   `json:"id"`
	CourseID  int64   `json:"course_id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	Size      int64   `json:"size"`
	Duration  float64 `json:"duration"`
	CreatedAt string  `json:"created_at"`
}

type Message struct {
	ID         int64  `json:"id"`
	SenderID   int64  `json:"sender_id"`
	ReceiverID int64  `json:"receiver_id"`
	Type       string `json:"type"` // text | image
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
	ReadAt     string `json:"read_at"`
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "coursehub.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite 单写者，避免锁冲突

	schema := `
CREATE TABLE IF NOT EXISTS users(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS courses(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  teacher_id INTEGER NOT NULL DEFAULT 1,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  invite_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS enrollments(
  course_id INTEGER NOT NULL,
  student_id INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(course_id, student_id)
);
CREATE TABLE IF NOT EXISTS videos(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'uploading',
  size INTEGER NOT NULL DEFAULT 0,
  duration REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_videos_course ON videos(course_id);
CREATE TABLE IF NOT EXISTS messages(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sender_id INTEGER NOT NULL,
  receiver_id INTEGER NOT NULL,
  type TEXT NOT NULL DEFAULT 'text',
  content TEXT NOT NULL,
  created_at TEXT NOT NULL,
  read_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_messages_pair ON messages(sender_id, receiver_id, id);`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// v1 → v2 迁移：courses 补列（列已存在时报错，忽略即可）
	db.Exec(`ALTER TABLE courses ADD COLUMN teacher_id INTEGER NOT NULL DEFAULT 1`)
	db.Exec(`ALTER TABLE courses ADD COLUMN invite_code TEXT NOT NULL DEFAULT ''`)
	return &Store{db: db}, nil
}

// Close 关闭数据库连接（测试与优雅退出使用）
func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// ---------------- 用户 ----------------

const userCols = `id, username, password_hash, display_name, role, status, created_at`

// 带 JOIN 的查询用这个：users 与其它表都有 id/created_at，必须加前缀
const userColsU = `u.id, u.username, u.password_hash, u.display_name, u.role, u.status, u.created_at`

func (s *Store) CreateUser(u *User) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO users(username, password_hash, display_name, role, status, created_at)
		VALUES(?,?,?,?,?,?)`, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.Status, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

func (s *Store) GetUser(id int64) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) SetUserStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) SetUserPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

// ---------------- 课程 ----------------

const courseCols = `c.id, c.teacher_id, IFNULL(t.display_name, t.username), c.title, c.description,
	c.invite_code, c.created_at`

const courseSelect = `SELECT ` + courseCols + `,
	(SELECT COUNT(*) FROM videos v WHERE v.course_id = c.id),
	(SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)
	FROM courses c LEFT JOIN users t ON t.id = c.teacher_id `

func scanCourse(row interface{ Scan(...any) error }) (*Course, error) {
	var c Course
	if err := row.Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Title, &c.Description,
		&c.InviteCode, &c.CreatedAt, &c.VideoCount, &c.StudentN); err != nil {
		return nil, err
	}
	return &c, nil
}

func collectCourses(rows *sql.Rows) ([]Course, error) {
	defer rows.Close()
	var out []Course
	for rows.Next() {
		c, err := scanCourse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) ListCoursesAll() ([]Course, error) {
	rows, err := s.db.Query(courseSelect + ` ORDER BY c.id DESC`)
	if err != nil {
		return nil, err
	}
	return collectCourses(rows)
}

func (s *Store) ListCoursesByTeacher(teacherID int64) ([]Course, error) {
	rows, err := s.db.Query(courseSelect+` WHERE c.teacher_id = ? ORDER BY c.id DESC`, teacherID)
	if err != nil {
		return nil, err
	}
	return collectCourses(rows)
}

func (s *Store) ListCoursesForStudent(studentID int64) ([]Course, error) {
	rows, err := s.db.Query(courseSelect+` JOIN enrollments e ON e.course_id = c.id
		WHERE e.student_id = ? ORDER BY c.id DESC`, studentID)
	if err != nil {
		return nil, err
	}
	return collectCourses(rows)
}

func (s *Store) GetCourse(id int64) (*Course, error) {
	return scanCourse(s.db.QueryRow(courseSelect+` WHERE c.id = ?`, id))
}

func (s *Store) GetCourseByInviteCode(code string) (*Course, error) {
	return scanCourse(s.db.QueryRow(courseSelect+` WHERE c.invite_code = ?`, code))
}

func (s *Store) CreateCourse(teacherID int64, title, description, inviteCode string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO courses(teacher_id, title, description, invite_code, created_at)
		VALUES(?,?,?,?,?)`, teacherID, title, description, inviteCode, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteCourse(courseID int64) error {
	for _, q := range []string{
		`DELETE FROM videos WHERE course_id = ?`,
		`DELETE FROM enrollments WHERE course_id = ?`,
		`DELETE FROM courses WHERE id = ?`,
	} {
		if _, err := s.db.Exec(q, courseID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetCourseTeacher(courseID, teacherID int64) error {
	_, err := s.db.Exec(`UPDATE courses SET teacher_id = ? WHERE id = ?`, teacherID, courseID)
	return err
}

func (s *Store) SetInviteCode(courseID int64, code string) error {
	_, err := s.db.Exec(`UPDATE courses SET invite_code = ? WHERE id = ?`, code, courseID)
	return err
}

func (s *Store) AddEnrollment(courseID, studentID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO enrollments(course_id, student_id, created_at) VALUES(?,?,?)`,
		courseID, studentID, now())
	return err
}

func (s *Store) IsEnrolled(courseID, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM enrollments WHERE course_id = ? AND student_id = ?`,
		courseID, userID).Scan(&n)
	return n > 0, err
}

func (s *Store) CourseStudents(courseID int64) ([]User, error) {
	rows, err := s.db.Query(`SELECT `+userColsU+` FROM users u
		JOIN enrollments e ON e.student_id = u.id WHERE e.course_id = ? ORDER BY u.id`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// CanChat 判定两人能否私聊：管理员对所有人开放；老师与其课程内学员互通
func (s *Store) CanChat(a, b *User) bool {
	if a.Role == "admin" || b.Role == "admin" {
		return true
	}
	teacher, student := a, b
	if teacher.Role != "teacher" {
		teacher, student = b, a
	}
	if teacher.Role != "teacher" || student.Role != "student" {
		return false
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM enrollments e
		JOIN courses c ON c.id = e.course_id
		WHERE c.teacher_id = ? AND e.student_id = ?`, teacher.ID, student.ID).Scan(&n)
	return n > 0
}

// ChatContacts 我的可聊天对象
func (s *Store) ChatContacts(me *User) ([]User, error) {
	var rows *sql.Rows
	var err error
	// 带 JOIN 的查询必须给列加 u. 前缀，否则 created_at 等列名有歧义
	switch me.Role {
	case "admin":
		rows, err = s.db.Query(`SELECT `+userCols+` FROM users WHERE id != ? AND status = 'active' ORDER BY role, id`, me.ID)
	case "teacher":
		rows, err = s.db.Query(`SELECT DISTINCT `+userColsU+` FROM users u
			JOIN enrollments e ON e.student_id = u.id
			JOIN courses c ON c.id = e.course_id
			WHERE c.teacher_id = ? AND u.status = 'active' ORDER BY u.id`, me.ID)
	default: // student：我加入课程的老师们
		rows, err = s.db.Query(`SELECT DISTINCT `+userColsU+` FROM users u
			JOIN courses c ON c.teacher_id = u.id
			JOIN enrollments e ON e.course_id = c.id
			WHERE e.student_id = ? AND u.status = 'active' ORDER BY u.id`, me.ID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ---------------- 消息 ----------------

func (s *Store) InsertMessage(senderID, receiverID int64, typ, content string) (*Message, error) {
	created := now()
	res, err := s.db.Exec(`INSERT INTO messages(sender_id, receiver_id, type, content, created_at)
		VALUES(?,?,?,?,?)`, senderID, receiverID, typ, content, created)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Message{ID: id, SenderID: senderID, ReceiverID: receiverID, Type: typ, Content: content, CreatedAt: created}, nil
}

func (s *Store) ChatHistory(me, peer, beforeID int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, sender_id, receiver_id, type, content, created_at, read_at
		FROM messages WHERE ((sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?))
		AND (? = 0 OR id < ?) ORDER BY id DESC LIMIT ?`,
		me, peer, peer, me, beforeID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.ReceiverID, &m.Type, &m.Content, &m.CreatedAt, &m.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MarkRead(me, peer int64) error {
	_, err := s.db.Exec(`UPDATE messages SET read_at = ? WHERE receiver_id = ? AND sender_id = ? AND read_at = ''`,
		now(), me, peer)
	return err
}

// UnreadCounts 返回 {对方用户ID: 未读条数}
func (s *Store) UnreadCounts(me int64) (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT sender_id, COUNT(*) FROM messages
		WHERE receiver_id = ? AND read_at = '' GROUP BY sender_id`, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var from, n int64
		if err := rows.Scan(&from, &n); err != nil {
			return nil, err
		}
		out[from] = n
	}
	return out, rows.Err()
}

// ---------------- 视频 ----------------

func (s *Store) VideoIDsOfCourse(courseID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM videos WHERE course_id = ?`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) CourseVideos(courseID int64) ([]Video, error) {
	rows, err := s.db.Query(`SELECT id, course_id, title, status, size, duration, created_at
		FROM videos WHERE course_id = ? ORDER BY id DESC`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(&v.ID, &v.CourseID, &v.Title, &v.Status, &v.Size, &v.Duration, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CreateVideo(courseID int64, title string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO videos(course_id, title, status, created_at) VALUES(?,?,'uploading',?)`,
		courseID, title, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetVideo(id int64) (*Video, error) {
	var v Video
	err := s.db.QueryRow(`SELECT id, course_id, title, status, size, duration, created_at
		FROM videos WHERE id = ?`, id).
		Scan(&v.ID, &v.CourseID, &v.Title, &v.Status, &v.Size, &v.Duration, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) CompleteVideo(id int64, size int64, duration float64) error {
	_, err := s.db.Exec(`UPDATE videos SET status = 'ready', size = ?, duration = ? WHERE id = ?`,
		size, duration, id)
	return err
}

func (s *Store) DeleteVideo(id int64) error {
	_, err := s.db.Exec(`DELETE FROM videos WHERE id = ?`, id)
	return err
}
