package main

// 严格作业 / 考卷模式与监考记录
//
// 设计要点：
//   - 严格模式按「练习」开启：限时、全屏、离开次数上限、摄像头/屏幕抓拍；
//   - 学员端在开始答题时上报一次 start（记录设备信息），答题过程中离开页面/退出全屏
//     会回报 leave 事件并附一张抓拍（有摄像头/屏幕共享权限时）；服务端按事件计数；
//   - 超过允许离开次数后学员端自动交卷，服务端在提交时给出违规/超时判定；
//   - 老师端可以查看每个学员的退出次数、用时、IP、浏览器、屏幕尺寸、时间线与截图。
//
// 与 practices 主表解耦：配置、事件各用一张表，避免影响已有库结构。

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------- 模型 ----------------

// ProctorConfig 严格模式配置（mode=normal 时其余字段不生效）
type ProctorConfig struct {
	Mode              string `json:"mode"`               // normal | strict
	MaxExits          int    `json:"max_exits"`          // 允许离开次数（0 = 一次都不允许）
	RequireFullscreen bool   `json:"require_fullscreen"` // 要求全屏答题
	RequireCamera     bool   `json:"require_camera"`     // 要求摄像头（用于抓拍）
	RequireScreen     bool   `json:"require_screen"`     // 要求共享屏幕（浏览器支持时）
}

func defaultProctorConfig() ProctorConfig {
	return ProctorConfig{Mode: "normal", MaxExits: 0, RequireFullscreen: true}
}

func (c ProctorConfig) valid() bool {
	if c.Mode != "normal" && c.Mode != "strict" {
		return false
	}
	return c.MaxExits >= 0 && c.MaxExits <= 20
}

// ProctorDetail 事件附带的环境信息（浏览器 / 屏幕 / 原因等）
type ProctorDetail struct {
	Reason   string `json:"reason,omitempty"` // blur | visibility | fullscreen | timer | manual
	UA       string `json:"ua,omitempty"`
	Screen   string `json:"screen,omitempty"`   // 1920x1080
	Window   string `json:"window,omitempty"`   // 1280x720
	Language string `json:"language,omitempty"` // zh-CN
	Timezone string `json:"timezone,omitempty"` // Asia/Shanghai
	Platform string `json:"platform,omitempty"`
	DPR      string `json:"dpr,omitempty"`
	Media    string `json:"media,omitempty"` // camera | screen
	Granted  bool   `json:"granted,omitempty"`
	IP       string `json:"ip,omitempty"`
	At       string `json:"at,omitempty"`
	Extra    string `json:"extra,omitempty"`

	// 交卷时由服务端写入的判定信息
	Late        bool `json:"late,omitempty"`
	Exits       int  `json:"exits,omitempty"`
	DurationSec int  `json:"duration_sec,omitempty"`
}

// ProctorEvent 一条监考事件
type ProctorEvent struct {
	ID         int64           `json:"id"`
	PracticeID int64           `json:"practice_id"`
	StudentID  int64           `json:"student_id"`
	Kind       string          `json:"kind"` // start | leave | return | copy | paste | snapshot | media | submit
	Detail     json.RawMessage `json:"detail,omitempty"`
	ImageURL   string          `json:"image_url,omitempty"`
	ImageKey   string          `json:"-"`
	CreatedAt  string          `json:"created_at"`
}

// ProctorAttempt 学员当前答卷状态（随练习详情返回，前端据此续答/计时）
type ProctorAttempt struct {
	StartedAt string `json:"started_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Exits     int    `json:"exits"`
	MaxExits  int    `json:"max_exits"`
	Over      bool   `json:"over"`
}

// ProctorStudentOverview 老师视角：单个学员的答卷与监考汇总
type ProctorStudentOverview struct {
	StudentID     int64   `json:"student_id"`
	StudentName   string  `json:"student_name"`
	Started       bool    `json:"started"`
	Submitted     bool    `json:"submitted"`
	Graded        bool    `json:"graded"`
	Score         float64 `json:"score"`
	Total         float64 `json:"total"`
	StartedAt     string  `json:"started_at,omitempty"`
	SubmittedAt   string  `json:"submitted_at,omitempty"`
	DurationUsed  int     `json:"duration_used"` // 秒
	ExitCount     int     `json:"exit_count"`
	SnapshotCount int     `json:"snapshot_count"`
	EventCount    int     `json:"event_count"`
	ProctorStatus string  `json:"proctor_status"` // not_started | in_progress | ok | warning | violation | late
	IP            string  `json:"ip,omitempty"`
	UserAgent     string  `json:"user_agent,omitempty"`
	Screen        string  `json:"screen,omitempty"`
	LastEventAt   string  `json:"last_event_at,omitempty"`
}

// ProctorEventKind 学员端可上报的事件类型（start/submit 由服务端产生）
var proctorEventKinds = map[string]bool{
	"leave": true, "return": true, "copy": true, "paste": true,
	"snapshot": true, "media": true, "heartbeat": true,
}

// ---------------- 建表 ----------------

func (s *Store) ensurePracticeProctorSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS practice_proctor(
  practice_id INTEGER PRIMARY KEY,
  mode TEXT NOT NULL DEFAULT 'normal',
  max_exits INTEGER NOT NULL DEFAULT 0,
  require_fullscreen INTEGER NOT NULL DEFAULT 1,
  require_camera INTEGER NOT NULL DEFAULT 0,
  require_screen INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS practice_proctor_events(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  practice_id INTEGER NOT NULL,
  student_id INTEGER NOT NULL,
  kind TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '{}',
  image_key TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_proctor_events_student ON practice_proctor_events(practice_id, student_id, id);
CREATE INDEX IF NOT EXISTS idx_proctor_events_practice ON practice_proctor_events(practice_id, id);
`)
	if err != nil {
		return err
	}
	// 老库补列：老师语音评语（列已存在时报错，忽略）
	for _, ddl := range []string{
		`ALTER TABLE practice_submissions ADD COLUMN feedback_audio TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE practice_submissions ADD COLUMN feedback_audio_duration REAL NOT NULL DEFAULT 0`,
	} {
		_, _ = s.db.Exec(ddl)
	}
	return nil
}

// ---------------- 配置读写 ----------------

func (s *Store) GetProctorConfig(practiceID int64) ProctorConfig {
	cfg := defaultProctorConfig()
	var mode string
	var maxExits, fullscreen, camera, screen int
	err := s.db.QueryRow(`SELECT mode, max_exits, require_fullscreen, require_camera, require_screen
		FROM practice_proctor WHERE practice_id = ?`, practiceID).
		Scan(&mode, &maxExits, &fullscreen, &camera, &screen)
	if err != nil {
		return cfg
	}
	cfg.Mode = mode
	cfg.MaxExits = maxExits
	cfg.RequireFullscreen = fullscreen == 1
	cfg.RequireCamera = camera == 1
	cfg.RequireScreen = screen == 1
	return cfg
}

func (s *Store) SaveProctorConfig(practiceID int64, cfg ProctorConfig) error {
	_, err := s.db.Exec(`INSERT INTO practice_proctor(practice_id, mode, max_exits, require_fullscreen, require_camera, require_screen)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(practice_id) DO UPDATE SET
			mode = excluded.mode, max_exits = excluded.max_exits,
			require_fullscreen = excluded.require_fullscreen, require_camera = excluded.require_camera,
			require_screen = excluded.require_screen`,
		practiceID, cfg.Mode, cfg.MaxExits, boolInt(cfg.RequireFullscreen),
		boolInt(cfg.RequireCamera), boolInt(cfg.RequireScreen))
	return err
}

func (s *Store) DeletePracticeProctor(practiceID int64) {
	_, _ = s.db.Exec(`DELETE FROM practice_proctor WHERE practice_id = ?`, practiceID)
	_, _ = s.db.Exec(`DELETE FROM practice_proctor_events WHERE practice_id = ?`, practiceID)
}

// ---------------- 事件读写 ----------------

func (s *Store) AddProctorEvent(practiceID, studentID int64, kind, detail, imageKey string) (int64, error) {
	if kind == "" {
		return 0, fmt.Errorf("事件类型不能为空")
	}
	res, err := s.db.Exec(`INSERT INTO practice_proctor_events(practice_id, student_id, kind, detail, image_key, created_at)
		VALUES(?,?,?,?,?,?)`, practiceID, studentID, kind, detail, imageKey, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ProctorStart 幂等开始答卷：已有 start 事件则直接返回首次开始时间
func (s *Store) ProctorStart(practiceID, studentID int64, detail string) (string, error) {
	var at string
	err := s.db.QueryRow(`SELECT created_at FROM practice_proctor_events
		WHERE practice_id = ? AND student_id = ? AND kind = 'start' ORDER BY id LIMIT 1`,
		practiceID, studentID).Scan(&at)
	if err == nil {
		return at, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	at = now()
	if _, err := s.db.Exec(`INSERT INTO practice_proctor_events(practice_id, student_id, kind, detail, created_at)
		VALUES(?,?,'start',?,?)`, practiceID, studentID, detail, at); err != nil {
		return "", err
	}
	return at, nil
}

func (s *Store) ProctorExitCount(practiceID, studentID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM practice_proctor_events
		WHERE practice_id = ? AND student_id = ? AND kind = 'leave'`, practiceID, studentID).Scan(&n)
	return n, err
}

func (s *Store) ProctorEventByID(id int64) (*ProctorEvent, error) {
	var ev ProctorEvent
	var detail, imageKey string
	err := s.db.QueryRow(`SELECT id, practice_id, student_id, kind, detail, image_key, created_at
		FROM practice_proctor_events WHERE id = ?`, id).
		Scan(&ev.ID, &ev.PracticeID, &ev.StudentID, &ev.Kind, &detail, &imageKey, &ev.CreatedAt)
	if err != nil {
		return nil, err
	}
	if detail != "" {
		ev.Detail = json.RawMessage(detail)
	}
	ev.ImageKey = imageKey
	if imageKey != "" {
		ev.ImageURL = "/api/proctor/snapshot/" + strconv.FormatInt(ev.ID, 10)
	}
	return &ev, nil
}

func (s *Store) ListProctorEvents(practiceID, studentID int64) ([]ProctorEvent, error) {
	rows, err := s.db.Query(`SELECT id, practice_id, student_id, kind, detail, image_key, created_at
		FROM practice_proctor_events WHERE practice_id = ? AND student_id = ? ORDER BY id`, practiceID, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProctorEvent, 0)
	for rows.Next() {
		var ev ProctorEvent
		var detail, imageKey string
		if err := rows.Scan(&ev.ID, &ev.PracticeID, &ev.StudentID, &ev.Kind, &detail, &imageKey, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if detail != "" {
			ev.Detail = json.RawMessage(detail)
		}
		ev.ImageKey = imageKey
		if imageKey != "" {
			ev.ImageURL = "/api/proctor/snapshot/" + strconv.FormatInt(ev.ID, 10)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// proctorEventAgg 单个学员的事件统计
type proctorEventAgg struct {
	startedAt time.Time
	lastEvent string
	exits     int
	snapshots int
	events    int
	startRaw  string
	ip        string
	ua        string
	screen    string
}

func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// ---------------- 老师视角汇总 ----------------

// ProctorOverview 老师监考台：本练习所有学员（含未提交）的提交与监考汇总
func (s *Store) ProctorOverview(practiceID int64) ([]ProctorStudentOverview, error) {
	var courseID int64
	var deadline string
	var durationMin int
	if err := s.db.QueryRow(`SELECT course_id, deadline, duration FROM practices WHERE id = ?`, practiceID).
		Scan(&courseID, &deadline, &durationMin); err != nil {
		return nil, err
	}
	cfg := s.GetProctorConfig(practiceID)

	type studentRow struct {
		id   int64
		name string
	}
	rows, err := s.db.Query(`SELECT u.id, COALESCE(NULLIF(u.display_name, ''), u.username)
		FROM enrollments e JOIN users u ON u.id = e.student_id
		WHERE e.course_id = ? ORDER BY u.id`, courseID)
	if err != nil {
		return nil, err
	}
	students := make([]studentRow, 0)
	for rows.Next() {
		var r studentRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			rows.Close()
			return nil, err
		}
		students = append(students, r)
	}
	rows.Close()

	subs, err := s.ListSubmissions(practiceID)
	if err != nil {
		return nil, err
	}
	subByStudent := make(map[int64]PracticeSubmission, len(subs))
	for _, sub := range subs {
		subByStudent[sub.StudentID] = sub
	}

	evRows, err := s.db.Query(`SELECT student_id, kind, detail, image_key, created_at
		FROM practice_proctor_events WHERE practice_id = ? ORDER BY id`, practiceID)
	if err != nil {
		return nil, err
	}
	aggs := map[int64]*proctorEventAgg{}
	for evRows.Next() {
		var sid int64
		var kind, detail, imageKey, createdAt string
		if err := evRows.Scan(&sid, &kind, &detail, &imageKey, &createdAt); err != nil {
			evRows.Close()
			return nil, err
		}
		agg := aggs[sid]
		if agg == nil {
			agg = &proctorEventAgg{}
			aggs[sid] = agg
		}
		agg.events++
		agg.lastEvent = createdAt
		switch kind {
		case "start":
			agg.startRaw = detail
			if t, ok := parseRFC3339(createdAt); ok {
				agg.startedAt = t
			}
		case "leave":
			agg.exits++
		case "snapshot":
			agg.snapshots++
		}
		if imageKey != "" {
			agg.snapshots++
		}
	}
	evRows.Close()
	for _, agg := range aggs {
		var d ProctorDetail
		if agg.startRaw != "" {
			_ = json.Unmarshal([]byte(agg.startRaw), &d)
			agg.ip, agg.ua, agg.screen = d.IP, d.UA, d.Screen
		}
	}

	out := make([]ProctorStudentOverview, 0, len(students))
	for _, st := range students {
		item := ProctorStudentOverview{StudentID: st.id, StudentName: st.name}
		agg := aggs[st.id]
		if agg != nil {
			item.ExitCount = agg.exits
			item.SnapshotCount = agg.snapshots
			item.EventCount = agg.events
			item.LastEventAt = agg.lastEvent
			item.IP, item.UserAgent, item.Screen = agg.ip, agg.ua, agg.screen
			if !agg.startedAt.IsZero() {
				item.Started = true
				item.StartedAt = agg.startedAt.Format(time.RFC3339)
			}
		}
		if sub, ok := subByStudent[st.id]; ok {
			item.Submitted = true
			item.Graded = sub.Graded
			item.Score, item.Total = sub.Score, sub.Total
			item.SubmittedAt = sub.SubmittedAt
			if item.Started {
				if subAt, ok := parseRFC3339(sub.SubmittedAt); ok {
					item.DurationUsed = int(subAt.Sub(agg.startedAt).Seconds())
					if item.DurationUsed < 0 {
						item.DurationUsed = 0
					}
				}
			}
		}
		item.ProctorStatus = proctorStatus(cfg, item, deadline, durationMin)
		out = append(out, item)
	}
	return out, nil
}

// proctorStatus 计算监考结论
func proctorStatus(cfg ProctorConfig, item ProctorStudentOverview, deadline string, durationMin int) string {
	if cfg.Mode != "strict" {
		if item.Submitted {
			return "ok"
		}
		if item.Started {
			return "in_progress"
		}
		return "not_started"
	}
	// 违规判定：离开次数超过允许值
	if item.ExitCount > cfg.MaxExits {
		return "violation"
	}
	// 超时判定：限时 + 宽限 2 分钟
	if item.Submitted && item.Started && durationMin > 0 {
		if subAt, ok := parseRFC3339(item.SubmittedAt); ok {
			start, _ := parseRFC3339(item.StartedAt)
			if subAt.After(start.Add(time.Duration(durationMin)*time.Minute + 2*time.Minute)) {
				return "late"
			}
		}
	}
	if item.Submitted {
		if item.ExitCount > 0 {
			return "warning"
		}
		return "ok"
	}
	if item.Started {
		return "in_progress"
	}
	return "not_started"
}

// ProctorStartedAt 学员首次开始答题的时间（没有则返回空）
func (s *Store) ProctorStartedAt(practiceID, studentID int64) string {
	var at string
	err := s.db.QueryRow(`SELECT created_at FROM practice_proctor_events
		WHERE practice_id = ? AND student_id = ? AND kind = 'start' ORDER BY id LIMIT 1`,
		practiceID, studentID).Scan(&at)
	if err != nil {
		return ""
	}
	return at
}

// ProctorAttemptInfo 学员答卷状态（随练习详情返回）
func (s *Store) ProctorAttemptInfo(practiceID, studentID int64) ProctorAttempt {
	cfg := s.GetProctorConfig(practiceID)
	att := ProctorAttempt{MaxExits: cfg.MaxExits}
	var startedAt string
	err := s.db.QueryRow(`SELECT created_at FROM practice_proctor_events
		WHERE practice_id = ? AND student_id = ? AND kind = 'start' ORDER BY id LIMIT 1`,
		practiceID, studentID).Scan(&startedAt)
	if err != nil {
		return att
	}
	att.StartedAt = startedAt
	exits, _ := s.ProctorExitCount(practiceID, studentID)
	att.Exits = exits
	att.Over = exits > cfg.MaxExits
	return att
}

// ---------------- 截图存取 ----------------

// saveProctorSnapshot 解析 dataURL 图片并写入存储，返回 key
func (a *API) saveProctorSnapshot(practiceID, studentID int64, dataURL string) (string, error) {
	dataURL = strings.TrimSpace(dataURL)
	const prefix = "data:image/"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", fmt.Errorf("截图格式无效")
	}
	comma := strings.IndexByte(dataURL, ',')
	if comma < 0 || !strings.Contains(dataURL[:comma], ";base64") {
		return "", fmt.Errorf("截图格式无效")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[comma+1:])
	if err != nil {
		return "", fmt.Errorf("截图解码失败")
	}
	if len(raw) < 128 {
		return "", fmt.Errorf("截图无效")
	}
	if len(raw) > 900<<10 {
		return "", fmt.Errorf("截图过大")
	}
	ext := ".jpg"
	if strings.Contains(dataURL[:comma], "png") {
		ext = ".png"
	} else if strings.Contains(dataURL[:comma], "webp") {
		ext = ".webp"
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	key := fmt.Sprintf("proctor/%d/%d/%s%s", practiceID, studentID, hexStr(buf), ext)
	if err := a.stg().Put(context.Background(), key, raw); err != nil {
		return "", err
	}
	return key, nil
}

// proctorDetailJSON 生成事件 detail JSON
func proctorDetailJSON(d ProctorDetail) string {
	if d.At == "" {
		d.At = time.Now().Format(time.RFC3339)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(b)
}
