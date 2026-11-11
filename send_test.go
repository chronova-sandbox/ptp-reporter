package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 指定したステータスを返すサーバを立てて、flush の後にバッファに残った行数を返す。
func flushAgainst(t *testing.T, status int, reply string) (left int, gotReq *http.Request, gotBody string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotReq, gotBody = r, string(b)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()

	cfg := config{url: srv.URL, database: "ptp", token: "apiv3_test"}
	buf := &buffer{}
	buf.add("m,host=a v=1i 1")
	buf.add("m,host=a v=2i 2")
	flush(cfg, buf)
	return len(buf.lines), gotReq, gotBody
}

func TestFlushRequestShape(t *testing.T) {
	left, req, body := flushAgainst(t, http.StatusNoContent, "")
	if left != 0 {
		t.Fatalf("成功したのにバッファに %d 行残っている", left)
	}
	if req.URL.Path != "/api/v3/write_lp" {
		t.Errorf("パス %q", req.URL.Path)
	}
	if q := req.URL.Query(); q.Get("db") != "ptp" || q.Get("precision") != "nanosecond" {
		t.Errorf("クエリ %v", q)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer apiv3_test" {
		t.Errorf("Authorization %q", got)
	}
	if body != "m,host=a v=1i 1\nm,host=a v=2i 2" {
		t.Errorf("本文 %q", body)
	}
}

func TestFlushKeepsOnTemporaryFailure(t *testing.T) {
	for _, status := range []int{500, 503, 429, 408} {
		if left, _, _ := flushAgainst(t, status, "busy"); left != 2 {
			t.Errorf("HTTP %d: バッファに %d 行(期待 2)。一時的な失敗は保持して再送する", status, left)
		}
	}
}

func TestFlushDropsOnPermanentFailure(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422} {
		if left, _, _ := flushAgainst(t, status, "invalid"); left != 0 {
			t.Errorf("HTTP %d: バッファに %d 行(期待 0)。再送しても通らないので捨てる", status, left)
		}
	}
}

func TestFlushKeepsWhenServerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // 閉じたので、接続できない

	buf := &buffer{}
	buf.add("m,host=a v=1i 1")
	flush(config{url: url, database: "ptp", token: "t"}, buf)
	if len(buf.lines) != 1 {
		t.Fatalf("接続できないとき、バッファに %d 行(期待 1)", len(buf.lines))
	}
}
