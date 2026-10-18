package main

import (
	"database/sql"
	"log"
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

// Settings 站点设置（单行，id=1），管理员可在后台修改
type Settings struct {
	SiteName      string `json:"site_name"`
	RegisterMode  string `json:"register_mode"`   // open | invite | closed
	OrgInviteCode string `json:"org_invite_code"` // invite 模式下注册需要

	// 存储后端（空 = 用环境变量 STORAGE_TYPE 等配置）
	StorageType string `json:"storage_type"` // ""(跟随环境变量) | local | s3 | webdav
	S3Endpoint  string `json:"s3_endpoint"`
	S3Bucket    string `json:"s3_bucket"`
	S3AccessKey string `json:"s3_access_key"`
	S3SecretKey string `json:"s3_secret_key"`
	S3UseSSL    bool   `json:"s3_use_ssl"`
	WebDAVURL   string `json:"webdav_url"`
	WebDAVUser  string `json:"webdav_username"`
	WebDAVPass  string `json:"webdav_password"`
}

type Course struct {
	ID           int64  `json:"id"`
	TeacherID    int64  `json:"teacher_id"`
	TeacherName  string `json:"teacher_name,omitempty"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	Cover        string `json:"cover"`       // 存储 key，空表示无封面
	CoverColor   string `json:"cover_color"` // 默认封面配色（green/blue/indigo/orange/pink/teal/slate/gold），空=按 ID 派生
	Published    bool   `json:"published"`
	InviteCode   string `json:"invite_code,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	LessonCount  int64  `json:"lesson_count"`
	StudentCount int64  `json:"student_count"`
	Enrolled     bool   `json:"enrolled"`   // 当前登录学员是否已加入
	CanManage    bool   `json:"can_manage"` // 当前登录用户能否管理（老师本人/管理员）
}

// Lesson 课时：视频 / 图文 / 文档
type Lesson struct {
	ID        int64   `json:"id"`
	CourseID  int64   `json:"course_id"`
	Chapter   string  `json:"chapter"`
	Title     string  `json:"title"`
	Kind      string  `json:"kind"`   // video | rich | doc
	Status    string  `json:"status"` // uploading | ready
	Size      int64   `json:"size"`
	Duration  float64 `json:"duration"`
	Content   string  `json:"content"`  // rich: HTML；doc: 存储 key
	Filename  string  `json:"filename"` // 文档原始文件名
	Mime      string  `json:"mime"`
	Position  int64   `json:"position"`
	CreatedAt string  `json:"created_at"`

	VideoMode string `json:"video_mode,omitempty"` // hls | mp4（后端计算，不入库）
	VideoURL  string `json:"video_url,omitempty"`  // 带有效期的签名播放地址
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

// GroupMessage 课程群聊消息，可引用某个课时的视频时间点
type GroupMessage struct {
	ID          int64   `json:"id"`
	CourseID    int64   `json:"course_id"`
	SenderID    int64   `json:"sender_id"`
	SenderName  string  `json:"sender_name,omitempty"`
	Type        string  `json:"type"` // text | image
	Content     string  `json:"content"`
	RefLessonID int64   `json:"ref_lesson_id,omitempty"`
	RefTime     float64 `json:"ref_time,omitempty"`
	RefTitle    string  `json:"ref_title,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// GroupSummary 群聊列表项
type GroupSummary struct {
	CourseID    int64  `json:"course_id"`
	Title       string `json:"title"`
	Cover       string `json:"cover"`
	TeacherName string `json:"teacher_name"`
	MemberCount int64  `json:"member_count"`
	LastID      int64  `json:"last_id"`
	LastContent string `json:"last_content"`
	LastType    string `json:"last_type"`
	LastSender  string `json:"last_sender"`
	LastAt      string `json:"last_at"`
	Unread      int64  `json:"unread"`
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
CREATE TABLE IF NOT EXISTS settings(
  id INTEGER PRIMARY KEY CHECK(id = 1),
  site_name TEXT NOT NULL DEFAULT '课程中心',
  register_mode TEXT NOT NULL DEFAULT 'open',
  org_invite_code TEXT NOT NULL DEFAULT '',
  storage_type TEXT NOT NULL DEFAULT '',
  s3_endpoint TEXT NOT NULL DEFAULT '',
  s3_bucket TEXT NOT NULL DEFAULT '',
  s3_access_key TEXT NOT NULL DEFAULT '',
  s3_secret_key TEXT NOT NULL DEFAULT '',
  s3_use_ssl INTEGER NOT NULL DEFAULT 0,
  webdav_url TEXT NOT NULL DEFAULT '',
  webdav_username TEXT NOT NULL DEFAULT '',
  webdav_password TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS courses(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  teacher_id INTEGER NOT NULL DEFAULT 1,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  cover TEXT NOT NULL DEFAULT '',
  cover_color TEXT NOT NULL DEFAULT '',
  published INTEGER NOT NULL DEFAULT 0,
  invite_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS enrollments(
  course_id INTEGER NOT NULL,
  student_id INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(course_id, student_id)
);
CREATE TABLE IF NOT EXISTS lessons(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  chapter TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'video',
  status TEXT NOT NULL DEFAULT 'uploading',
  size INTEGER NOT NULL DEFAULT 0,
  duration REAL NOT NULL DEFAULT 0,
  content TEXT NOT NULL DEFAULT '',
  filename TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_lessons_course ON lessons(course_id);
CREATE TABLE IF NOT EXISTS videos(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'uploading',
  size INTEGER NOT NULL DEFAULT 0,
  duration REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sender_id INTEGER NOT NULL,
  receiver_id INTEGER NOT NULL,
  type TEXT NOT NULL DEFAULT 'text',
  content TEXT NOT NULL,
  created_at TEXT NOT NULL,
  read_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_messages_pair ON messages(sender_id, receiver_id, id);
CREATE TABLE IF NOT EXISTS group_messages(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  sender_id INTEGER NOT NULL,
  type TEXT NOT NULL DEFAULT 'text',
  content TEXT NOT NULL,
  ref_lesson_id INTEGER NOT NULL DEFAULT 0,
  ref_time REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_group_messages_course ON group_messages(course_id, id);
CREATE TABLE IF NOT EXISTS group_reads(
  course_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  last_read_id INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(course_id, user_id)
);`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	// 旧版本库升级：courses 补列（列已存在时报错，忽略即可）
	db.Exec(`ALTER TABLE courses ADD COLUMN teacher_id INTEGER NOT NULL DEFAULT 1`)
	db.Exec(`ALTER TABLE courses ADD COLUMN invite_code TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE courses ADD COLUMN category TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE courses ADD COLUMN cover TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE courses ADD COLUMN cover_color TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE courses ADD COLUMN published INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE courses ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`)
	db.Exec(`UPDATE courses SET updated_at = created_at WHERE updated_at = ''`)

	// 存储后端设置（老库补齐列）
	for _, q := range []string{
		`ALTER TABLE settings ADD COLUMN storage_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN s3_endpoint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN s3_bucket TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN s3_access_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN s3_secret_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN s3_use_ssl INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE settings ADD COLUMN webdav_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN webdav_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE settings ADD COLUMN webdav_password TEXT NOT NULL DEFAULT ''`,
	} {
		db.Exec(q)
	}

	s := &Store{db: db}
	// 旧版本库升级：videos → lessons（保留原 id，HLS 存储目录因此不变）
	s.migrateVideosToLessons()
	return s, nil
}

// migrateVideosToLessons 老库的 videos 表数据复制成 lessons（kind=video）
func (s *Store) migrateVideosToLessons() {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&n); err != nil || n > 0 {
		return
	}
	s.db.Exec(`INSERT INTO lessons(id, course_id, chapter, title, kind, status, size, duration,
		content, filename, mime, position, created_at)
		SELECT id, course_id, '', title, 'video', status, size, duration, '', '', '', id, created_at FROM videos`)
}

// Close 关闭数据库连接（测试与优雅退出使用）
func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// ---------------- 设置 ----------------

func (s *Store) InitSettings(siteName, mode, code string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO settings(id, site_name, register_mode, org_invite_code, updated_at)
		VALUES(1,?,?,?,?)`, siteName, mode, code, now())
	return err
}

func (s *Store) GetSettings() *Settings {
	st := &Settings{SiteName: "课程中心", RegisterMode: "open"}
	row := s.db.QueryRow(`SELECT site_name, register_mode, org_invite_code,
		storage_type, s3_endpoint, s3_bucket, s3_access_key, s3_secret_key, s3_use_ssl,
		webdav_url, webdav_username, webdav_password FROM settings WHERE id = 1`)
	if err := row.Scan(&st.SiteName, &st.RegisterMode, &st.OrgInviteCode,
		&st.StorageType, &st.S3Endpoint, &st.S3Bucket, &st.S3AccessKey, &st.S3SecretKey, &st.S3UseSSL,
		&st.WebDAVURL, &st.WebDAVUser, &st.WebDAVPass); err != nil {
		return st
	}
	return st
}

func (s *Store) SaveSettings(st *Settings) error {
	_, err := s.db.Exec(`INSERT INTO settings(id, site_name, register_mode, org_invite_code,
		storage_type, s3_endpoint, s3_bucket, s3_access_key, s3_secret_key, s3_use_ssl,
		webdav_url, webdav_username, webdav_password, updated_at)
		VALUES(1,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET site_name = excluded.site_name,
			register_mode = excluded.register_mode, org_invite_code = excluded.org_invite_code,
			storage_type = excluded.storage_type, s3_endpoint = excluded.s3_endpoint,
			s3_bucket = excluded.s3_bucket, s3_access_key = excluded.s3_access_key,
			s3_secret_key = excluded.s3_secret_key, s3_use_ssl = excluded.s3_use_ssl,
			webdav_url = excluded.webdav_url, webdav_username = excluded.webdav_username,
			webdav_password = excluded.webdav_password,
			updated_at = excluded.updated_at`,
		st.SiteName, st.RegisterMode, st.OrgInviteCode,
		st.StorageType, st.S3Endpoint, st.S3Bucket, st.S3AccessKey, st.S3SecretKey, st.S3UseSSL,
		st.WebDAVURL, st.WebDAVUser, st.WebDAVPass, now())
	return err
}

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
	c.category, c.cover, c.cover_color, c.published, c.invite_code, c.created_at, c.updated_at`

const courseSelect = `SELECT ` + courseCols + `,
	(SELECT COUNT(*) FROM lessons l WHERE l.course_id = c.id),
	(SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)
	FROM courses c LEFT JOIN users t ON t.id = c.teacher_id `

func scanCourse(row interface{ Scan(...any) error }) (*Course, error) {
	var c Course
	if err := row.Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Title, &c.Description,
		&c.Category, &c.Cover, &c.CoverColor, &c.Published, &c.InviteCode, &c.CreatedAt, &c.UpdatedAt,
		&c.LessonCount, &c.StudentCount); err != nil {
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
	rows, err := s.db.Query(courseSelect+` JOIN enrollments en ON en.course_id = c.id
		WHERE en.student_id = ? ORDER BY c.id DESC`, studentID)
	if err != nil {
		return nil, err
	}
	courses, err := collectCourses(rows)
	if err != nil {
		return nil, err
	}
	for i := range courses {
		courses[i].Enrolled = true
	}
	return courses, nil
}

// ListCoursesMarket 已上架到市场的课程（学员可自主加入）
func (s *Store) ListCoursesMarket(studentID int64) ([]Course, error) {
	rows, err := s.db.Query(courseSelect + ` WHERE c.published = 1 ORDER BY c.id DESC`)
	if err != nil {
		return nil, err
	}
	courses, err := collectCourses(rows)
	if err != nil {
		return nil, err
	}
	if studentID > 0 {
		for i := range courses {
			ok, _ := s.IsEnrolled(courses[i].ID, studentID)
			courses[i].Enrolled = ok
		}
	}
	return courses, nil
}

func (s *Store) GetCourse(id int64) (*Course, error) {
	return scanCourse(s.db.QueryRow(courseSelect+` WHERE c.id = ?`, id))
}

func (s *Store) GetCourseByInviteCode(code string) (*Course, error) {
	return scanCourse(s.db.QueryRow(courseSelect+` WHERE c.invite_code = ?`, code))
}

func (s *Store) CreateCourse(teacherID int64, title, description, category, coverColor, inviteCode string) (int64, error) {
	t := now()
	res, err := s.db.Exec(`INSERT INTO courses(teacher_id, title, description, category, cover_color, invite_code, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, teacherID, title, description, category, coverColor, inviteCode, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateCourse(id int64, title, description, category, coverColor string) error {
	_, err := s.db.Exec(`UPDATE courses SET title = ?, description = ?, category = ?, cover_color = ?, updated_at = ? WHERE id = ?`,
		title, description, category, coverColor, now(), id)
	return err
}

func (s *Store) SetCourseCoverColor(id int64, color string) error {
	_, err := s.db.Exec(`UPDATE courses SET cover_color = ?, updated_at = ? WHERE id = ?`, color, now(), id)
	return err
}

func (s *Store) SetCoursePublished(id int64, published bool) error {
	_, err := s.db.Exec(`UPDATE courses SET published = ?, updated_at = ? WHERE id = ?`, published, now(), id)
	return err
}

func (s *Store) SetCourseCover(id int64, key string) error {
	_, err := s.db.Exec(`UPDATE courses SET cover = ?, updated_at = ? WHERE id = ?`, key, now(), id)
	return err
}

func (s *Store) DeleteCourse(courseID int64) error {
	for _, q := range []string{
		`DELETE FROM lessons WHERE course_id = ?`,
		`DELETE FROM enrollments WHERE course_id = ?`,
		`DELETE FROM group_messages WHERE course_id = ?`,
		`DELETE FROM group_reads WHERE course_id = ?`,
		`DELETE FROM courses WHERE id = ?`,
	} {
		if _, err := s.db.Exec(q, courseID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetCourseTeacher(courseID, teacherID int64) error {
	_, err := s.db.Exec(`UPDATE courses SET teacher_id = ?, updated_at = ? WHERE id = ?`, teacherID, now(), courseID)
	return err
}

func (s *Store) SetInviteCode(courseID int64, code string) error {
	_, err := s.db.Exec(`UPDATE courses SET invite_code = ?, updated_at = ? WHERE id = ?`, code, now(), courseID)
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

// ---------------- 课时 ----------------

const lessonCols = `id, course_id, chapter, title, kind, status, size, duration, content, filename, mime, position, created_at`

func scanLesson(row interface{ Scan(...any) error }) (*Lesson, error) {
	var l Lesson
	err := row.Scan(&l.ID, &l.CourseID, &l.Chapter, &l.Title, &l.Kind, &l.Status, &l.Size,
		&l.Duration, &l.Content, &l.Filename, &l.Mime, &l.Position, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) CourseLessons(courseID int64) ([]Lesson, error) {
	rows, err := s.db.Query(`SELECT `+lessonCols+` FROM lessons WHERE course_id = ? ORDER BY position, id`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lesson
	for rows.Next() {
		l, err := scanLesson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (s *Store) GetLesson(id int64) (*Lesson, error) {
	return scanLesson(s.db.QueryRow(`SELECT `+lessonCols+` FROM lessons WHERE id = ?`, id))
}

func (s *Store) CreateLesson(courseID int64, chapter, title, kind string) (int64, error) {
	var pos int64
	s.db.QueryRow(`SELECT IFNULL(MAX(position), 0) + 1 FROM lessons WHERE course_id = ?`, courseID).Scan(&pos)
	status := "uploading"
	if kind == "rich" {
		status = "ready"
	}
	res, err := s.db.Exec(`INSERT INTO lessons(course_id, chapter, title, kind, status, position, created_at)
		VALUES(?,?,?,?,?,?,?)`, courseID, chapter, title, kind, status, pos, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateLessonMeta(id int64, chapter, title string) error {
	_, err := s.db.Exec(`UPDATE lessons SET chapter = ?, title = ? WHERE id = ?`, chapter, title, id)
	return err
}

func (s *Store) UpdateLessonContent(id int64, content string) error {
	_, err := s.db.Exec(`UPDATE lessons SET content = ?, status = 'ready' WHERE id = ?`, content, id)
	return err
}

func (s *Store) SetLessonDoc(id int64, key, filename, mime string, size int64) error {
	_, err := s.db.Exec(`UPDATE lessons SET content = ?, filename = ?, mime = ?, size = ?, status = 'ready' WHERE id = ?`,
		key, filename, mime, size, id)
	return err
}

func (s *Store) CompleteLesson(id int64, size int64, duration float64) error {
	_, err := s.db.Exec(`UPDATE lessons SET status = 'ready', size = ?, duration = ? WHERE id = ?`,
		size, duration, id)
	return err
}

// SetLessonVideo 视频转码完成：content 指向 m3u8 或源文件
func (s *Store) SetLessonVideo(id int64, content string, size int64, duration float64) error {
	_, err := s.db.Exec(`UPDATE lessons SET content = ?, status = 'ready', size = ?, duration = ? WHERE id = ?`,
		content, size, duration, id)
	return err
}

func (s *Store) SetLessonStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE lessons SET status = ? WHERE id = ?`, status, id)
	return err
}

// ResetStuckLessons 启动时把卡在「转码中」的课时标记为失败（转码任务在进程内，重启不会自动续）
func (s *Store) ResetStuckLessons() {
	if res, err := s.db.Exec(`UPDATE lessons SET status = 'failed' WHERE status = 'processing'`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("有 %d 个课时重启前正在转码，已标记为失败（可删除后重新上传）", n)
		}
	}
}

func (s *Store) DeleteLesson(id int64) error {
	_, err := s.db.Exec(`DELETE FROM lessons WHERE id = ?`, id)
	return err
}

func (s *Store) LessonIDsOfCourse(courseID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM lessons WHERE course_id = ?`, courseID)
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

// ---------------- 私聊消息 ----------------

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

// ---------------- 课程群聊 ----------------

func (s *Store) InsertGroupMessage(courseID, senderID int64, typ, content string, refLessonID int64, refTime float64) (*GroupMessage, error) {
	created := now()
	res, err := s.db.Exec(`INSERT INTO group_messages(course_id, sender_id, type, content, ref_lesson_id, ref_time, created_at)
		VALUES(?,?,?,?,?,?,?)`, courseID, senderID, typ, content, refLessonID, refTime, created)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	gm := &GroupMessage{ID: id, CourseID: courseID, SenderID: senderID, Type: typ,
		Content: content, RefLessonID: refLessonID, RefTime: refTime, CreatedAt: created}
	if refLessonID > 0 {
		if l, err := s.GetLesson(refLessonID); err == nil {
			gm.RefTitle = l.Title
		}
	}
	return gm, nil
}

func (s *Store) GroupHistory(courseID, beforeID int64, limit int) ([]GroupMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT gm.id, gm.course_id, gm.sender_id, IFNULL(u.display_name, u.username),
			gm.type, gm.content, gm.ref_lesson_id, gm.ref_time, IFNULL(l.title, ''), gm.created_at
		FROM group_messages gm
		LEFT JOIN users u ON u.id = gm.sender_id
		LEFT JOIN lessons l ON l.id = gm.ref_lesson_id
		WHERE gm.course_id = ? AND (? = 0 OR gm.id < ?)
		ORDER BY gm.id DESC LIMIT ?`, courseID, beforeID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMessage
	for rows.Next() {
		var m GroupMessage
		if err := rows.Scan(&m.ID, &m.CourseID, &m.SenderID, &m.SenderName, &m.Type, &m.Content,
			&m.RefLessonID, &m.RefTime, &m.RefTitle, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MarkGroupRead(courseID, userID, lastID int64) error {
	if lastID <= 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO group_reads(course_id, user_id, last_read_id) VALUES(?,?,?)
		ON CONFLICT(course_id, user_id) DO UPDATE SET last_read_id = MAX(last_read_id, excluded.last_read_id)`,
		courseID, userID, lastID)
	return err
}

// GroupAudience 群聊可推送对象：任课老师 + 已加入学员
func (s *Store) GroupAudience(courseID int64) []int64 {
	var out []int64
	var teacherID int64
	if err := s.db.QueryRow(`SELECT teacher_id FROM courses WHERE id = ?`, courseID).Scan(&teacherID); err == nil && teacherID > 0 {
		out = append(out, teacherID)
	}
	rows, err := s.db.Query(`SELECT student_id FROM enrollments WHERE course_id = ?`, courseID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil && id != teacherID {
			out = append(out, id)
		}
	}
	return out
}

// CanGroupChat 当前用户能否在课程群里发言/查看
func (s *Store) CanGroupChat(u *User, courseID int64) bool {
	if u.Role == "admin" {
		return true
	}
	var teacherID int64
	if err := s.db.QueryRow(`SELECT teacher_id FROM courses WHERE id = ?`, courseID).Scan(&teacherID); err != nil {
		return false
	}
	if u.Role == "teacher" {
		return teacherID == u.ID
	}
	ok, _ := s.IsEnrolled(courseID, u.ID)
	return ok
}

// MyGroups 我参与的课程群（学员：已加入的课；老师：自己教的课；管理员：全部）
func (s *Store) MyGroups(me *User) ([]GroupSummary, error) {
	var rows *sql.Rows
	var err error
	switch me.Role {
	case "admin":
		rows, err = s.db.Query(`SELECT c.id, c.title, c.cover, IFNULL(t.display_name, t.username) FROM courses c
			LEFT JOIN users t ON t.id = c.teacher_id ORDER BY c.id DESC`)
	case "teacher":
		rows, err = s.db.Query(`SELECT c.id, c.title, c.cover, IFNULL(t.display_name, t.username) FROM courses c
			LEFT JOIN users t ON t.id = c.teacher_id WHERE c.teacher_id = ? ORDER BY c.id DESC`, me.ID)
	default:
		rows, err = s.db.Query(`SELECT c.id, c.title, c.cover, IFNULL(t.display_name, t.username) FROM courses c
			JOIN enrollments e ON e.course_id = c.id LEFT JOIN users t ON t.id = c.teacher_id
			WHERE e.student_id = ? ORDER BY c.id DESC`, me.ID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var courses []GroupSummary
	for rows.Next() {
		var g GroupSummary
		if err := rows.Scan(&g.CourseID, &g.Title, &g.Cover, &g.TeacherName); err != nil {
			return nil, err
		}
		courses = append(courses, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]GroupSummary, 0, len(courses))
	for _, g := range courses {
		s.db.QueryRow(`SELECT COUNT(*) FROM enrollments WHERE course_id = ?`, g.CourseID).Scan(&g.MemberCount)
		var last GroupMessage
		err := s.db.QueryRow(`SELECT gm.id, gm.type, gm.content, IFNULL(u.display_name, u.username), gm.created_at
			FROM group_messages gm LEFT JOIN users u ON u.id = gm.sender_id
			WHERE gm.course_id = ? ORDER BY gm.id DESC LIMIT 1`, g.CourseID).
			Scan(&last.ID, &last.Type, &last.Content, &last.SenderName, &last.CreatedAt)
		if err == nil {
			g.LastID, g.LastType, g.LastContent, g.LastSender, g.LastAt =
				last.ID, last.Type, last.Content, last.SenderName, last.CreatedAt
		}
		var lastRead int64
		s.db.QueryRow(`SELECT last_read_id FROM group_reads WHERE course_id = ? AND user_id = ?`,
			g.CourseID, me.ID).Scan(&lastRead)
		s.db.QueryRow(`SELECT COUNT(*) FROM group_messages WHERE course_id = ? AND id > ? AND sender_id <> ?`,
			g.CourseID, lastRead, me.ID).Scan(&g.Unread)
		out = append(out, g)
	}
	return out, nil
}

// LatestGroupMessageID 某课程群最新消息 ID（用于整体标记已读）
func (s *Store) LatestGroupMessageID(courseID int64) int64 {
	var id int64
	s.db.QueryRow(`SELECT IFNULL(MAX(id), 0) FROM group_messages WHERE course_id = ?`, courseID).Scan(&id)
	return id
}
