package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// /healthz 是 Docker HEALTHCHECK 的探针，改动它时确认探针依赖的 200 与 status 字段不变。
func TestHandleHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q，期望 ok", body["status"])
	}
	// 版本信息由 -ldflags 注入；测试里跑的是源码默认值，只要和变量一致即可
	if body["version"] != version {
		t.Errorf("version = %q，期望 %q", body["version"], version)
	}
	if body["commit"] != commit {
		t.Errorf("commit = %q，期望 %q", body["commit"], commit)
	}
	if body["build_time"] != buildTime {
		t.Errorf("build_time = %q，期望 %q", body["build_time"], buildTime)
	}
}
