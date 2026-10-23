package main

// TTS 朗读代理：转发鉴权头、内存缓存、文本校验、未登录拒绝、未配置 404

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTTSProxy(t *testing.T) {
	var calls int32
	var gotAuth, gotInput, gotVoice string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			Input string `json:"input"`
			Voice string `json:"voice"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotInput, gotVoice = req.Input, req.Voice
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("FAKE-MP3-" + req.Input))
	}))
	defer upstream.Close()

	api, handler := newTestAPI(t)
	api.cfg.TTSURL = upstream.URL + "/v1/audio/speech"
	api.cfg.TTSKey = "test-tts-key"
	api.cfg.TTSVoice = "en-US-TestNeural"

	srv := httptest.NewServer(handler)
	testBase = srv.URL
	defer srv.Close()

	admin := login(t, "admin", "admin-pass-123")

	// 首次请求：带 key 转发上游，音频原样返回
	resp, err := admin.Get(testBase + "/api/tts?text=" + url.QueryEscape("apple"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/mpeg" || string(b) != "FAKE-MP3-apple" {
		t.Fatalf("首次朗读异常: HTTP %d, CT %s, body %q", resp.StatusCode, resp.Header.Get("Content-Type"), b)
	}
	if gotAuth != "Bearer test-tts-key" || gotInput != "apple" || gotVoice != "en-US-TestNeural" {
		t.Fatalf("上游请求参数不对: auth=%q input=%q voice=%q", gotAuth, gotInput, gotVoice)
	}

	// 第二次请求同一文本：命中缓存，上游不再被调用
	resp2, err := admin.Get(testBase + "/api/tts?text=" + url.QueryEscape("apple"))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("缓存未生效，上游被调用了 %d 次", n)
	}

	// 未登录 → 401
	anon := newClient()
	if r, err := anon.Get(testBase + "/api/tts?text=apple"); err != nil || r.StatusCode != 401 {
		t.Fatalf("未登录应 401, got %v/%d", err, r.StatusCode)
	} else {
		r.Body.Close()
	}

	// 空文本 / 超长文本 → 400
	if r, err := admin.Get(testBase + "/api/tts?text=" + url.QueryEscape("   ")); err != nil || r.StatusCode != 400 {
		t.Fatalf("空文本应 400, got %v/%d", err, r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r, err := admin.Get(testBase + "/api/tts?text=" + url.QueryEscape(strings.Repeat("a", 300))); err != nil || r.StatusCode != 400 {
		t.Fatalf("超长文本应 400, got %v/%d", err, r.StatusCode)
	} else {
		r.Body.Close()
	}

	// 未配置 TTS_URL 的实例 → 404
	_, handler2 := newTestAPI(t)
	srv2 := httptest.NewServer(handler2)
	testBase = srv2.URL
	defer srv2.Close()
	admin2 := login(t, "admin", "admin-pass-123")
	if r, err := admin2.Get(testBase + "/api/tts?text=apple"); err != nil || r.StatusCode != 404 {
		t.Fatalf("未配置应 404, got %v/%d", err, r.StatusCode)
	} else {
		r.Body.Close()
	}
}
