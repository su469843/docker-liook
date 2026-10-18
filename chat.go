package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------- WebSocket Hub（私聊 + 课程群聊 + 通话信令） ----------------

type msgRef struct {
	LessonID int64   `json:"lesson_id"`
	Time     float64 `json:"time"`
}

type wsMsg struct {
	Type      string          `json:"type"` // chat.send | chat.read | group.send | group.read | call.offer | call.answer | call.ice | call.end
	To        int64           `json:"to"`
	Group     int64           `json:"group,omitempty"` // 课程群 ID（即课程 ID）
	Msg       *chatPayload    `json:"msg,omitempty"`
	Ref       *msgRef         `json:"ref,omitempty"` // 群聊引用：课时 + 视频时间点
	SDP       string          `json:"sdp,omitempty"`
	Video     bool            `json:"video,omitempty"` // 通话类型：true=视频，false=语音
	Candidate json.RawMessage `json:"candidate,omitempty"`
}

type chatPayload struct {
	Type    string `json:"type"` // text | image
	Content string `json:"content"`
}

type serverMsg struct {
	Type      string          `json:"type"` // chat.new | chat.sent | group.new | group.sent | call.* | presence | error
	From      *User           `json:"from,omitempty"`
	FromID    int64           `json:"from_id,omitempty"`
	Msg       *Message        `json:"msg,omitempty"`
	GMsg      *GroupMessage   `json:"gmsg,omitempty"`
	Group     int64           `json:"group,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Video     bool            `json:"video,omitempty"` // 邀请方是否发起视频通话
	Candidate json.RawMessage `json:"candidate,omitempty"`
	IDs       []int64         `json:"ids,omitempty"`
	Message   string          `json:"message,omitempty"`
	Online    bool            `json:"online,omitempty"`
}

type client struct {
	hub  *Hub
	user *User
	ws   *websocket.Conn
	send chan []byte
}

type Hub struct {
	api     *API
	mu      sync.Mutex
	clients map[int64]map[*client]struct{} // 一个账号允许多条连接（多个标签页 / 重连切换）
	busy    map[int64]int64                // userID → 正在通话的对方 ID（0=空闲）
}

func newHub(api *API) *Hub {
	return &Hub{api: api, clients: map[int64]map[*client]struct{}{}, busy: map[int64]int64{}}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	set := h.clients[c.user.ID]
	if set == nil {
		set = map[*client]struct{}{}
		h.clients[c.user.ID] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()
	h.broadcastPresence()
}

// callEndGrace 通话断线宽限期：信令连接短暂抖动（切网络、刷新页面）时先不挂断，
// 等用户重新连上再继续，避免「莫名挂断」。
var callEndGrace = 5 * time.Second

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	set := h.clients[c.user.ID]
	_, existed := set[c]
	if existed {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.user.ID)
		}
	}
	anyLeft := len(set) > 0
	peer := h.busy[c.user.ID]
	h.mu.Unlock()

	// 该账号还有其它连接在线（多个标签页 / 重连后的新连接）时，关闭旧连接不影响通话，
	// 否则「打开第二个标签页/刷新一下」就会把正在进行的通话挂掉。
	if !existed || anyLeft || peer <= 0 {
		h.broadcastPresence()
		return
	}
	uid := c.user.ID
	time.AfterFunc(callEndGrace, func() {
		h.mu.Lock()
		back := len(h.clients[uid]) > 0
		stillPeer := h.busy[uid]
		ended := !back && stillPeer == peer
		if ended {
			delete(h.busy, uid)
			delete(h.busy, peer)
		}
		h.mu.Unlock()
		if ended {
			// 宽限期内没有重连，才通知对方挂断
			h.sendTo(peer, serverMsg{Type: "call.end", FromID: uid, Message: "对方已断线"})
		}
	})
	h.broadcastPresence()
}

func (h *Hub) onlineIDs() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]int64, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// sendTo 把消息推给该账号的所有连接（多个标签页都能收到）。
func (h *Hub) sendTo(uid int64, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	h.mu.Lock()
	targets := make([]*client, 0, len(h.clients[uid]))
	for c := range h.clients[uid] {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	sent := false
	for _, c := range targets {
		select {
		case c.send <- b:
			sent = true
		default:
		}
	}
	return sent
}

func (h *Hub) broadcastPresence() {
	ids := h.onlineIDs()
	b, _ := json.Marshal(serverMsg{Type: "presence", IDs: ids})
	h.mu.Lock()
	for _, set := range h.clients {
		for c := range set {
			select {
			case c.send <- b:
			default:
			}
		}
	}
	h.mu.Unlock()
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		// 仅允许同源
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	},
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 45 * time.Second
	maxMsgSize = 1 << 20 // 1MB
)

func (a *API) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	uid, ok := parseSessionToken(a.cfg.Secret, c.Value)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "登录已过期")
		return
	}
	u, err := a.db.GetUser(uid)
	if err != nil || u.Status != "active" {
		writeErr(w, http.StatusUnauthorized, "账号无效")
		return
	}
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	cl := &client{hub: a.hub, user: u, ws: ws, send: make(chan []byte, 64)}
	a.hub.add(cl) // add() 内会向所有连接（含新连接）广播在线列表
	go cl.writePump()
	cl.readPump()
	a.hub.remove(cl)
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() { ticker.Stop(); c.ws.Close() }()
	for {
		select {
		case b, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) readPump() {
	defer c.ws.Close()
	c.ws.SetReadLimit(maxMsgSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		var m wsMsg
		if err := c.ws.ReadJSON(&m); err != nil {
			return
		}
		c.hub.handleMsg(c, &m)
	}
}

func (h *Hub) handleMsg(c *client, m *wsMsg) {
	switch m.Type {
	case "chat.send":
		h.handleChatSend(c, m)
	case "chat.read":
		if peer := mustUser(h.api, m.To); peer != nil && h.api.db.CanChat(c.user, peer) {
			h.api.db.MarkRead(c.user.ID, m.To)
		}
	case "group.send":
		h.handleGroupSend(c, m)
	case "group.read":
		if m.Group > 0 && h.api.db.CanGroupChat(c.user, m.Group) {
			h.api.db.MarkGroupRead(m.Group, c.user.ID, h.api.db.LatestGroupMessageID(m.Group))
		}
	case "call.offer", "call.answer", "call.ice", "call.end":
		h.handleCall(c, m)
	default:
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "未知消息类型"})
	}
}

func mustUser(a *API, id int64) *User {
	u, _ := a.db.GetUser(id)
	return u
}

func (h *Hub) handleChatSend(c *client, m *wsMsg) {
	if m.Msg == nil || m.To <= 0 || m.To == c.user.ID {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息参数无效"})
		return
	}
	if m.Msg.Type != "text" && m.Msg.Type != "image" {
		m.Msg.Type = "text"
	}
	content := strings.TrimSpace(m.Msg.Content)
	if content == "" || len(content) > 8000 {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息为空或超长"})
		return
	}
	peer, _ := h.api.db.GetUser(m.To)
	if peer == nil || peer.Status != "active" || !h.api.db.CanChat(c.user, peer) {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "无法向该用户发送消息"})
		return
	}
	saved, err := h.api.db.InsertMessage(c.user.ID, m.To, m.Msg.Type, content)
	if err != nil {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息保存失败"})
		return
	}
	// 回执给发送方（拿到 ID/时间）
	h.sendTo(c.user.ID, serverMsg{Type: "chat.sent", Msg: saved})
	// 实时推送给接收方（在线时）
	if online := h.sendTo(m.To, serverMsg{Type: "chat.new", From: publicUser(c.user), Msg: saved}); online {
		h.api.db.MarkRead(m.To, c.user.ID) // 对方在线且已推送，视为已读
	}
}

// handleGroupSend 课程群聊：所有成员可见，可引用某课时的视频时间点
func (h *Hub) handleGroupSend(c *client, m *wsMsg) {
	if m.Group <= 0 || m.Msg == nil {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息参数无效"})
		return
	}
	if !h.api.db.CanGroupChat(c.user, m.Group) {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "你不在此课程群中"})
		return
	}
	typ := "text"
	if m.Msg.Type == "image" {
		typ = "image"
	}
	content := strings.TrimSpace(m.Msg.Content)
	if content == "" || len(content) > 4000 {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息为空或超长"})
		return
	}
	var refLessonID int64
	var refTime float64
	if m.Ref != nil && m.Ref.LessonID > 0 {
		if l, err := h.api.db.GetLesson(m.Ref.LessonID); err == nil && l.CourseID == m.Group {
			refLessonID = l.ID
			refTime = m.Ref.Time
			if refTime < 0 {
				refTime = 0
			}
			if refTime > 24*3600 {
				refTime = 24 * 3600
			}
		}
	}
	gm, err := h.api.db.InsertGroupMessage(m.Group, c.user.ID, typ, content, refLessonID, refTime)
	if err != nil {
		h.sendTo(c.user.ID, serverMsg{Type: "error", Message: "消息保存失败"})
		return
	}
	gm.SenderName = c.user.DisplayName
	h.sendTo(c.user.ID, serverMsg{Type: "group.sent", Group: m.Group, GMsg: gm})
	for _, uid := range h.api.db.GroupAudience(m.Group) {
		if uid == c.user.ID {
			continue
		}
		h.sendTo(uid, serverMsg{Type: "group.new", Group: m.Group, From: publicUser(c.user), GMsg: gm})
	}
}

func (h *Hub) handleCall(c *client, m *wsMsg) {
	if m.To <= 0 || m.To == c.user.ID {
		return
	}
	h.mu.Lock()
	onlineTarget := len(h.clients[m.To]) > 0
	busyTarget := h.busy[m.To]
	busyMe := h.busy[c.user.ID]
	h.mu.Unlock()

	switch m.Type {
	case "call.offer":
		if !onlineTarget {
			h.sendTo(c.user.ID, serverMsg{Type: "call.busy", FromID: m.To, Message: "对方不在线"})
			return
		}
		if busyTarget > 0 && busyTarget != c.user.ID {
			h.sendTo(c.user.ID, serverMsg{Type: "call.busy", FromID: m.To, Message: "对方正在通话中"})
			return
		}
		if busyMe > 0 && busyMe != m.To {
			h.sendTo(c.user.ID, serverMsg{Type: "call.busy", FromID: busyMe, Message: "你已在通话中"})
			return
		}
		h.mu.Lock()
		h.busy[c.user.ID] = m.To
		h.busy[m.To] = c.user.ID
		h.mu.Unlock()
		h.sendTo(m.To, serverMsg{Type: "call.offer", From: publicUser(c.user), SDP: m.SDP, Video: m.Video})
	case "call.answer":
		if busyMe == m.To {
			h.sendTo(m.To, serverMsg{Type: "call.answer", From: publicUser(c.user), SDP: m.SDP, Video: m.Video})
		}
	case "call.ice":
		if busyMe == m.To || busyTarget == c.user.ID {
			h.sendTo(m.To, serverMsg{Type: "call.ice", FromID: c.user.ID, Candidate: m.Candidate})
		}
	case "call.end":
		h.mu.Lock()
		delete(h.busy, c.user.ID)
		delete(h.busy, m.To)
		h.mu.Unlock()
		h.sendTo(m.To, serverMsg{Type: "call.end", FromID: c.user.ID})
	}
}

func publicUser(u *User) *User {
	cp := *u
	return &cp
}

// ---------------- 聊天 REST ----------------

func (a *API) handleContacts(w http.ResponseWriter, r *http.Request, u *User) {
	users, err := a.db.ChatContacts(u)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	unread, _ := a.db.UnreadCounts(u.ID)
	online := map[int64]bool{}
	for _, id := range a.hub.onlineIDs() {
		online[id] = true
	}
	type contact struct {
		User
		Unread int64 `json:"unread"`
		Online bool  `json:"online"`
	}
	out := []contact{}
	for _, cu := range users {
		out = append(out, contact{User: cu, Unread: unread[cu.ID], Online: online[cu.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleChatHistory(w http.ResponseWriter, r *http.Request, u *User) {
	peerID, err := strconv.ParseInt(r.PathValue("peer"), 10, 64)
	if err != nil || peerID <= 0 {
		writeErr(w, 400, "无效用户")
		return
	}
	peer, err := a.db.GetUser(peerID)
	if err != nil || !a.db.CanChat(u, peer) {
		writeErr(w, http.StatusForbidden, "无法与该用户聊天")
		return
	}
	before := int64(0)
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = b
	}
	msgs, err := a.db.ChatHistory(u.ID, peerID, before, 50)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	a.db.MarkRead(u.ID, peerID)
	// 前端希望旧→新排序
	if msgs == nil {
		msgs = []Message{}
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (a *API) handleMyGroups(w http.ResponseWriter, r *http.Request, u *User) {
	groups, err := a.db.MyGroups(u)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if groups == nil {
		groups = []GroupSummary{}
	}
	writeJSON(w, http.StatusOK, groups)
}

func (a *API) handleGroupHistory(w http.ResponseWriter, r *http.Request, u *User) {
	courseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || courseID <= 0 {
		writeErr(w, 400, "无效课程")
		return
	}
	if !a.db.CanGroupChat(u, courseID) {
		writeErr(w, http.StatusForbidden, "你不在该课程群中")
		return
	}
	before := int64(0)
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = b
	}
	msgs, err := a.db.GroupHistory(courseID, before, 50)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if before == 0 {
		a.db.MarkGroupRead(courseID, u.ID, a.db.LatestGroupMessageID(courseID))
	}
	if msgs == nil {
		msgs = []GroupMessage{}
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (a *API) handleGroupRead(w http.ResponseWriter, r *http.Request, u *User) {
	courseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || courseID <= 0 {
		writeErr(w, 400, "无效课程")
		return
	}
	if !a.db.CanGroupChat(u, courseID) {
		writeErr(w, http.StatusForbidden, "你不在该课程群中")
		return
	}
	var req struct {
		LastID int64 `json:"last_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.LastID <= 0 {
		req.LastID = a.db.LatestGroupMessageID(courseID)
	}
	a.db.MarkGroupRead(courseID, u.ID, req.LastID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ICE 服务器配置（前端建立 WebRTC 连接用）
func (a *API) handleIce(w http.ResponseWriter, r *http.Request, u *User) {
	ice := []map[string]any{
		{"urls": []string{"stun:stun.l.google.com:19302"}},
	}
	if a.cfg.TurnURL != "" {
		urls := []string{}
		for _, u := range strings.Split(a.cfg.TurnURL, ",") {
			if s := strings.TrimSpace(u); s != "" {
				urls = append(urls, s)
			}
		}
		if len(urls) > 0 {
			ice = append(ice, map[string]any{
				"urls":       urls,
				"username":   a.cfg.TurnUser,
				"credential": a.cfg.TurnPass,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"iceServers": ice})
}
