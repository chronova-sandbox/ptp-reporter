package main

import (
	"fmt"
	"regexp"
	"strconv"
)

// 行頭が "[" の行(journalctl -o cat で出る本文)を拾う。ptp4l と phc2sys は、本文の内容で区別する。
var (
	rePHC = regexp.MustCompile(
		`^\[[\d.]+\] (\S+) (?:sys|phc) offset\s+(-?\d+) s(\d) freq\s+([+-]\d+) delay\s+(\d+)`)
	rePTP = regexp.MustCompile(
		`^\[[\d.]+\] rms\s+(\d+) max\s+(\d+) freq\s+([+-]\d+) \+/-\s+(\d+) delay\s+(-?\d+)`)
	// --summary_interval を付けない ptp4l が、同期のたびに出す行(符号付きのオフセット)。
	// path delay は出ない場合もあるので、省略可能にしてある。
	reMaster = regexp.MustCompile(
		`^\[[\d.]+\] master offset\s+(-?\d+) s(\d) freq\s+([+-]\d+)(?: path delay\s+(-?\d+))?`)
)

// num は、正規表現で取り出した数字の文字列を整数にする
func num(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// toLineProtocol は、ログ 1 行を InfluxDB の line protocol に変換する。
func toLineProtocol(line, host string, tsNs int64) (lp string, ok bool) {
	if m := rePHC.FindStringSubmatch(line); m != nil {
		return fmt.Sprintf("ptp_phc2sys,host=%s,target=%s offset_ns=%di,state=%di,freq_ppb=%di,delay_ns=%di %d",
			host, m[1], num(m[2]), num(m[3]), num(m[4]), num(m[5]), tsNs), true
	}

	if m := reMaster.FindStringSubmatch(line); m != nil {
		fields := fmt.Sprintf("offset_ns=%di,state=%di,freq_ppb=%di", num(m[1]), num(m[2]), num(m[3]))
		if m[4] != "" { // path delay は省略可能
			fields += fmt.Sprintf(",delay_ns=%di", num(m[4]))
		}
		return fmt.Sprintf("ptp_ptp4l,host=%s %s %d", host, fields, tsNs), true
	}

	if m := rePTP.FindStringSubmatch(line); m != nil {
		return fmt.Sprintf("ptp_ptp4l,host=%s rms_ns=%di,max_ns=%di,freq_ppb=%di,delay_ns=%di %d",
			host, num(m[1]), num(m[2]), num(m[3]), num(m[5]), tsNs), true
	}

	return "", false
}
