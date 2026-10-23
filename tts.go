package main

// TTS 朗读代理：把自建 Worker TTS 藏在服务端。
//
// 前端只调 GET /api/tts?text=...（带登录态），服务器带着真正的 API key
// （环境变量 TTS_URL / TTS_KEY，只在运行时存在，不进前端产物、不进仓库）
// 去请求 OpenAI 兼容的 Worker 端点，把 MP3 流回浏览器。
//   - 音频在服务器内存里做全站缓存：同一单词只向上游合成一次，命中后零上游流量；
//   - 每用户令牌桶限流，防止登录用户把代理当免费 TTS 农场；
//   - 未配置 TTS_URL 时接口返回 404，前端自动退回 Worker 直连 / 浏览器内置发音。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ttsMaxText  = 200 // 单次朗读文本上限（rune）
	ttsMaxAudio = 2 << 20
	ttsCacheMax = 512 // 缓存条数上限（每条约 10-30KB MP3，最多十几 MB 内存）
)

// ttsCache 音频内存缓存：text -> MP3 字节
type ttsCache struct {
	mu    sync.Mutex
	items map[string][]byte
}

func (c *ttsCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.items[key]
	return b, ok
}

func (c *ttsCache) put(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= ttsCacheMax {
		c.items = map[string][]byte{} // 简单粗暴：满了整表清空，反正下一轮很快回填
	}
	c.items[key] = b
}

// handleTTS GET /api/tts?text=...  朗读代理（登录用户）
func (a *API) handleTTS(w http.ResponseWriter, r *http.Request, u *User) {
	cfg := a.cfg
	if cfg == nil || cfg.TTSURL == "" {
		writeErr(w, http.StatusNotFound, "TTS 未配置")
		return
	}
	// 页面加载会并发预拉整份题目的音频，突发给大一点；持续速率压低防刷
	if !a.rate().allow("tts:"+fmt.Sprint(u.ID), 2, 150) {
		writeErr(w, http.StatusTooManyRequests, "朗读太频繁，稍后再试")
		return
	}
	text := strings.Join(strings.Fields(r.URL.Query().Get("text")), " ")
	if text == "" || utf8.RuneCountInString(text) > ttsMaxText {
		writeErr(w, http.StatusBadRequest, "朗读内容为空或过长")
		return
	}

	if b, ok := a.tts().get(text); ok {
		serveTTSAudio(w, b)
		return
	}

	payload, err := json.Marshal(map[string]any{
		"model": "tts-1", "input": text, "voice": cfg.TTSVoice,
		"response_format": "mp3", "speed": cfg.TTSSpeed,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "TTS 请求构造失败")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TTSURL, bytes.NewReader(payload))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "TTS 请求构造失败")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.TTSKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.TTSKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "TTS 服务不可用")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "audio/") {
		writeErr(w, http.StatusBadGateway, "TTS 服务返回异常")
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, ttsMaxAudio))
	if err != nil || len(b) == 0 {
		writeErr(w, http.StatusBadGateway, "TTS 音频读取失败")
		return
	}
	a.tts().put(text, b)
	serveTTSAudio(w, b)
}

func serveTTSAudio(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "audio/mpeg")
	// 只缓存在学员自己的浏览器里（带登录态的接口不宜共享缓存）
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	_, _ = w.Write(b)
}
