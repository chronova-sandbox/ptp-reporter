package main

import "testing"

func TestToLineProtocol(t *testing.T) {
	const ts = 1794121899211428000
	tests := []struct {
		name string
		line string
		want string // 空文字列は「対象外の行」
	}{
		{
			"ptp4l の rms の行",
			"[2159.227] rms 3200 max 3218 freq +23458 +/- 495 delay   320 +/-   2",
			"ptp_ptp4l,host=rpi2 rms_ns=3200i,max_ns=3218i,freq_ppb=23458i,delay_ns=320i 1794121899211428000",
		},
		{
			"負の遅延",
			"[1601.219] rms 39522 max 41123 freq +25365 +/- 4533 delay  -450 +/- 554",
			"ptp_ptp4l,host=rpi2 rms_ns=39522i,max_ns=41123i,freq_ppb=25365i,delay_ns=-450i 1794121899211428000",
		},
		{
			"master offset(path delay あり)",
			"[19219.099] master offset         32 s2 freq  -56389 path delay       372",
			"ptp_ptp4l,host=rpi2 offset_ns=32i,state=2i,freq_ppb=-56389i,delay_ns=372i 1794121899211428000",
		},
		{
			"master offset(負の値)",
			"[1234.567] master offset         -12 s2 freq   +1234 path delay       350",
			"ptp_ptp4l,host=rpi2 offset_ns=-12i,state=2i,freq_ppb=1234i,delay_ns=350i 1794121899211428000",
		},
		{
			"master offset(path delay なし)",
			"[1236.567] master offset      -80123 s1 freq   -5000",
			"ptp_ptp4l,host=rpi2 offset_ns=-80123i,state=1i,freq_ppb=-5000i 1794121899211428000",
		},
		{
			"phc2sys(CLOCK_REALTIME)",
			"[2159.358] CLOCK_REALTIME phc offset     -3169 s2 freq  +89782 delay    999",
			"ptp_phc2sys,host=rpi2,target=CLOCK_REALTIME offset_ns=-3169i,state=2i,freq_ppb=89782i,delay_ns=999i 1794121899211428000",
		},
		{
			"実機の ptp4l",
			"[4576.845] master offset        -96 s2 freq  -53433 path delay       381",
			"ptp_ptp4l,host=rpi2 offset_ns=-96i,state=2i,freq_ppb=-53433i,delay_ns=381i 1794121899211428000",
		},
		{
			"実機の phc2sys(rpi1、CLOCK_REALTIME を基準)",
			"[4620.652] eth0 sys offset       -12 s2 freq  -53605 delay    981",
			"ptp_phc2sys,host=rpi2,target=eth0 offset_ns=-12i,state=2i,freq_ppb=-53605i,delay_ns=981i 1794121899211428000",
		},
		{
			"phc2sys(eth0 sys)",
			"[695.235] eth0 sys offset     -3312 s2 freq  +31375 delay   1037",
			"ptp_phc2sys,host=rpi2,target=eth0 offset_ns=-3312i,state=2i,freq_ppb=31375i,delay_ns=1037i 1794121899211428000",
		},
		{
			"32 ビット整数を超える巨大なオフセット",
			"[1458.311] eth0 sys offset -11693832501187 s1 freq  -61232 delay   1037",
			"ptp_phc2sys,host=rpi2,target=eth0 offset_ns=-11693832501187i,state=1i,freq_ppb=-61232i,delay_ns=1037i 1794121899211428000",
		},
		{"関係ない行", "[219.578] port 1: link down", ""},
		{"空行", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toLineProtocol(tt.line, "rpi2", ts)
			if tt.want == "" {
				if ok {
					t.Fatalf("対象外の行が変換された: %q", got)
				}
				return
			}
			if !ok || got != tt.want {
				t.Fatalf("\n got: %q (ok=%v)\nwant: %q", got, ok, tt.want)
			}
		})
	}
}

func TestBufferTrimsOldestFirst(t *testing.T) {
	b := &buffer{}
	for i := 0; i < maxBuffer+5; i++ {
		b.add(string(rune('a' + i%26)))
	}
	if got := len(b.lines); got != maxBuffer {
		t.Fatalf("長さ %d, 期待 %d", got, maxBuffer)
	}
	// 失敗して戻した分も、上限を超えたら古い(先頭の)ものから捨てられる
	b.restoreFront([]string{"x", "y", "z"})
	if got := len(b.lines); got != maxBuffer {
		t.Fatalf("restoreFront 後の長さ %d, 期待 %d", got, maxBuffer)
	}
}
