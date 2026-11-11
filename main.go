// ptp-reporter は、スレーブ の ptp4l / phc2sys のログを読み、InfluxDB 3 に送る
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	flushInterval = 5 * time.Second // この間隔でまとめて送る
	maxBuffer     = 10000           // 送れないときに貯めておく最大行数
)

type config struct {
	host, url, database, token string
	units                      []string
}

func loadConfig() config {
	return config{
		host:     os.Getenv("HOST_TAG"),
		url:      strings.TrimRight(os.Getenv("INFLUX_URL"), "/"),
		database: os.Getenv("INFLUX_DATABASE"),
		token:    os.Getenv("INFLUX_TOKEN"),
		units:    strings.Split(os.Getenv("PTP_UNITS"), ","),
	}
}

// buffer は、送信待ちの line protocol を貯める
type buffer struct {
	mu    sync.Mutex
	lines []string
}

// trim は、上限を超えた分を古いものから捨てる
func (b *buffer) trim() {
	if n := len(b.lines) - maxBuffer; n > 0 {
		b.lines = append([]string(nil), b.lines[n:]...)
	}
}

func (b *buffer) add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	b.trim()
}

// takeAll は、貯まっている行をすべて取り出して、バッファを空にする。
func (b *buffer) takeAll() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.lines
	b.lines = nil
	return out
}

// restoreFront は、送れなかった行をバッファの先頭に戻す。
func (b *buffer) restoreFront(rest []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(append([]string(nil), rest...), b.lines...)
	b.trim()
}

// sender は、flushInterval ごとに flush を呼ぶ。
func sender(ctx context.Context, cfg config, buf *buffer) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			flush(cfg, buf)
		}
	}
}

// readLines は、journalctl の出力を 1 行ずつ handle に渡す。
func readLines(ctx context.Context, units []string, handle func(string)) {
	var args []string
	for _, u := range units {
		args = append(args, "-u", u)
	}
	args = append(args, "-f", "-n", "0", "-o", "cat")

	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "journalctl", args...)
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "journalctl を起動できません(%v)\n", err)
		} else {
			sc := bufio.NewScanner(stdout)
			for sc.Scan() {
				handle(sc.Text())
			}
			_ = cmd.Wait()
		}
		if ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "journalctl が終了しました。3 秒後に再起動します")
			time.Sleep(3 * time.Second)
		}
	}
}

func main() {
	cfg := loadConfig()
	if cfg.host == "" || cfg.url == "" || cfg.database == "" || cfg.token == "" || cfg.units[0] == "" {
		fmt.Fprintln(os.Stderr, "HOST_TAG, INFLUX_URL, INFLUX_DATABASE, INFLUX_TOKEN, PTP_UNITS を環境変数で指定してください")
		os.Exit(1)
	}

	// systemctl stop / restart(SIGTERM)や Ctrl+C で ctx が終わり、readLines が戻る
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	buf := &buffer{}
	go sender(ctx, cfg, buf)

	readLines(ctx, cfg.units, func(raw string) {
		if lp, ok := toLineProtocol(strings.TrimSpace(raw), cfg.host, time.Now().UnixNano()); ok {
			buf.add(lp)
		}
	})

	flush(cfg, buf) // 停止するとき、残りを送り切る
}
