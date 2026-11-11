package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// sendError は、サーバが 2xx 以外を返したことを表す。body はサーバの説明
type sendError struct {
	status int
	body   string
}

func (e *sendError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
}

// retryable は、同じ内容をもう一度送れば通る可能性があるかを返す。
func retryable(err error) bool {
	se, ok := err.(*sendError)
	if !ok {
		return true
	}
	return se.status >= 500 || se.status == http.StatusRequestTimeout || se.status == http.StatusTooManyRequests
}

// post は、line protocol を InfluxDB 3 の書き込み API(/api/v3/write_lp)に送る。
func post(cfg config, body string) error {
	q := url.Values{"db": {cfg.database}, "precision": {"nanosecond"}}
	req, err := http.NewRequest(http.MethodPost, cfg.url+"/api/v3/write_lp?"+q.Encode(), strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode/100 != 2 {
		return &sendError{status: res.StatusCode, body: strings.TrimSpace(string(msg))}
	}
	return nil
}

// flush は、貯まった行をまとめて送る。
func flush(cfg config, buf *buffer) {
	batch := buf.takeAll()
	if len(batch) == 0 {
		return
	}
	err := post(cfg, strings.Join(batch, "\n"))
	switch {
	case err == nil:
	case retryable(err):
		fmt.Fprintf(os.Stderr, "送信に失敗しました(%v)。%d 行を保持して再送します\n", err, len(batch))
		buf.restoreFront(batch)
	default:
		fmt.Fprintf(os.Stderr, "送信が拒否されました(%v)。再送しても通らないので、%d 行を捨てました\n", err, len(batch))
	}
}
