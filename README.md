# ptp-reporter

PTP スレーブ(rpi2)の `ptp4l` / `phc2sys` のログを読み、InfluxDB 3 に送る。Go の標準ライブラリだけで書いてある。

```
ptp4l-slave / phc2sys-slave → journald → ptp-reporter → HTTP POST → InfluxDB 3 → Grafana
```

- 受け取る側(InfluxDB 3 と Grafana)は、[ptp-dashboard-grafana](https://github.com/chronova-sandbox/ptp-dashboard-grafana)。
- rpi への配置(systemd ユニットと環境ファイル)は、[ptp-rpi-ansible](https://github.com/chronova-sandbox/ptp-rpi-ansible)。

## 動作

`journalctl -u <ユニット> -f -n 0 -o cat` で、ログを 1 行ずつ読む。

- 変換できる行は、`ptp4l` の `master offset`(`rms` の要約行も)と、`phc2sys` の `offset`。それ以外の行は、無視する。
- 行の頭は `[4576.845] master offset ...` の形(`-o cat` で本文だけが出る)を想定している。`ptp4l` / `phc2sys` に `-m` を付けると、`ptp4l[4576.845]: ...` の形の行も出て、**同じ内容が二重になる**。ただし、この形の行は、変換の対象にしていない。ユニットには `-m` を付けない。
- タイムスタンプは、reporter が動く機器の時刻(`time.Now()`)を使う。
- 5 秒ごとに、貯まった行をまとめて `POST /api/v3/write_lp` で送る。

### 送信に失敗したとき

| 状況 | 動作 |
|---|---|
| 接続できない、5xx、408、429 | バッファに戻して、次の周期に再送する(最大 10000 行。超えたら、古いものから捨てる) |
| それ以外の 4xx(400 のスキーマ違反、401 のトークン違いなど) | **再送しても通らないので、その行を捨てて**、サーバの返事をログに出す |

400 を戻すと、以後のデータも詰まって、何も書き込まれなくなる。そのため、捨てる。理由は、`journalctl -u ptp-reporter` のログに出る。

## データの形

InfluxDB 3 のテーブルと列は、最初の書き込みで決まる。**列の型は、あとから変えられない**(変えるなら、テーブルを作り直す)。整数はすべて `i` を付けて送る。

| テーブル | タグ | フィールド(すべて整数) |
|---|---|---|
| `ptp_ptp4l` | `host` | `offset_ns`、`state`、`freq_ppb`、`delay_ns`(省略される行もある) |
| `ptp_ptp4l`(`rms` の要約行) | `host` | `rms_ns`、`max_ns`、`freq_ppb`、`delay_ns` |
| `ptp_phc2sys` | `host`、`target` | `offset_ns`、`state`、`freq_ppb`、`delay_ns` |

`state` は、サーボの状態(0 = 未ロック、1 = ステップ、2 = ロック)。

## 設定(環境変数)

省略時のデフォルトはない。足りないと、起動時にエラーで終了する。

| 変数 | 例 | 内容 |
|---|---|---|
| `HOST_TAG` | `rpi2` | `host` タグの値 |
| `INFLUX_URL` | `http://192.168.x.x:8181` | InfluxDB 3 の URL |
| `INFLUX_DATABASE` | `ptp` | 書き込み先のデータベース名 |
| `INFLUX_TOKEN` | `apiv3_...` | 書き込みの権限があるトークン(`Authorization: Bearer`) |
| `PTP_UNITS` | `ptp4l-slave,phc2sys-slave` | 読む systemd ユニット(カンマ区切り) |

## テスト

```bash
go vet ./... && go test ./...
```

`parse_test.go` は、実機のログの形の行が、期待する line protocol に変換されるかを確かめる。`send_test.go` は、`httptest` で、リクエストの形と、再送するか捨てるかの判断を確かめる。

## ビルド

rpi2 は、カーネルが 64 ビットで、**ユーザーランドが 32 ビット**(`getconf LONG_BIT` が 32)。そのため、`arm`(armv7)向けを使う。Go の `int` は 32 ビットになるので、`parse.go` では `int64` を使っている(起動直後の phc2sys には、`-11693832501187` のような巨大な offset が出る)。

```bash
# rpi2(32 ビットのユーザーランド)
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/ptp-reporter-linux-armv7 .

# 64 ビットのユーザーランドの機器
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/ptp-reporter-linux-arm64 .
```

`dist/` は Git に入れない(`.gitignore` 済み)。

## リリース

バイナリは、GitHub Releases に置く。rpi へは、Ansible の `get_url` で、チェックサムを確かめて配る。

```bash
go vet ./... && go test ./...
# 上のビルドを実行してから
cd dist && shasum -a 256 ptp-reporter-linux-* > SHA256SUMS && cd ..
git tag v0.1.0 && git push origin v0.1.0
gh release create v0.1.0 dist/ptp-reporter-linux-* dist/SHA256SUMS --title v0.1.0 --notes "..."
```

## 手動で試す

rpi2 で、環境変数を渡して動かす。

```bash
HOST_TAG=rpi2 INFLUX_URL=http://<サーバ>:8181 INFLUX_DATABASE=ptp INFLUX_TOKEN=apiv3_... \
PTP_UNITS=ptp4l-slave,phc2sys-slave ./ptp-reporter
```

動かしたまま、別の端末で、データを確かめる。

```bash
curl -s -H "Authorization: Bearer $INFLUX_TOKEN" -H 'Content-Type: application/json' \
  http://<サーバ>:8181/api/v3/query_sql \
  -d '{"db":"ptp","q":"SELECT * FROM ptp_ptp4l ORDER BY time DESC LIMIT 3","format":"json"}'
```

データが出ないとき、次を見る。

- `journalctl -u ptp4l-slave -n 5 -o cat` の行の形が、`parse.go` の正規表現と合っているか。
- reporter のログに、`送信が拒否されました(HTTP 401 ...)`(トークン違い)や `HTTP 400 ...`(型の不一致)が出ていないか。
