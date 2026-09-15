// Command verify 是 docker compose 中的一次性验收服务：
// 对真实运行的 Go API 与 Vue（nginx）前端做端到端断言，全部通过才以 0 退出。
// 它不使用任何固定结果——期望值均由本程序按公式独立复算得出。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	apiBase = env("API_BASE", "http://api:8080")
	webBase = env("WEB_BASE", "http://web")
)

var (
	passed int
	failed int
	client = &http.Client{Timeout: 10 * time.Second}
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func check(name string, ok bool, detail ...any) {
	if ok {
		passed++
		fmt.Printf("  ✓ %s\n", name)
		return
	}
	failed++
	fmt.Printf("  ✗ %s\n", name)
	for _, d := range detail {
		fmt.Printf("      %v\n", d)
	}
}

func postJSON(url string, payload any) (int, map[string]any, []byte) {
	body, _ := json.Marshal(payload)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		failed++
		fmt.Printf("  ✗ 请求失败 %s: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, raw
}

type band struct {
	Frequency   int     `json:"frequency"`
	Level       float64 `json:"level"`
	Attenuation float64 `json:"attenuation"`
}

func bands(ls, as []float64) []band {
	fs := []int{125, 250, 500, 1000, 2000, 4000}
	bs := make([]band, 6)
	for i := range fs {
		bs[i] = band{fs[i], ls[i], as[i]}
	}
	return bs
}

// 测试侧独立复算：E = 10·log10(Σ10^(C/10))
func combine(cs []float64) float64 {
	sum := 0.0
	for _, c := range cs {
		sum += math.Pow(10, c/10)
	}
	return 10 * math.Log10(sum)
}

func main() {
	fmt.Println("耳罩合成声级：一次性验收（真实 Go API + Vue/nginx 链路）")

	// 1) API 健康检查
	fmt.Println("[1] API 健康检查")
	resp, err := client.Get(apiBase + "/health")
	if err != nil {
		fmt.Printf("  ✗ 无法连接 API %s: %v\n", apiBase, err)
		os.Exit(1)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	check("GET /health 返回 200", resp.StatusCode == 200, "status:", resp.StatusCode)
	check("健康响应为 {\"status\":\"ok\"}", strings.Contains(string(body), `"ok"`), string(body))

	// 2) 合法整单：六行 C 全为 80，E = 80 + 10·log10(6) = 87.7815… → 87.8
	fmt.Println("[2] 合法整单的真实计算")
	valid := bands(
		[]float64{90, 92, 95.5, 100, 98, 94},
		[]float64{10, 12, 15.5, 20, 18, 14},
	)
	status, out, raw := postJSON(apiBase+"/api/calculate", map[string]any{"bands": valid})
	check("POST /api/calculate 返回 200", status == 200, "status:", status, string(raw))

	rows, _ := out["rows"].([]any)
	check("返回 6 行结果", len(rows) == 6, "rows:", len(rows))
	csOK := true
	for _, r := range rows {
		rm := r.(map[string]any)
		c := rm["corrected"].(float64)
		if math.Abs(c-80.0) > 1e-9 {
			csOK = false
		}
	}
	check("六个 C = L − A 均为 80.0", csOK)

	exact, exactOK := out["exactLevel"].(float64)
	wantExact := combine([]float64{80, 80, 80, 80, 80, 80})
	check("未舍入值与独立复算一致（< 1e-9）", exactOK && math.Abs(exact-wantExact) < 1e-9,
		"api:", exact, "recomputed:", wantExact)
	display, _ := out["displayLevel"].(string)
	check("显示值为四舍五入一位小数 87.8", display == "87.8", "display:", display)
	_, hasSub := out["substitution"].(string)
	check("返回公式代入依据", hasSub)

	// 3) 整单校验：缺行 / 额外频带 / 越界 / 两位小数 / 无穷值 / 缺字段
	fmt.Println("[3] 非法整单一律 422，且不返回任何部分结果")
	expect422 := func(name string, payload any, wantField string) {
		st, errBody, rb := postJSON(apiBase+"/api/calculate", payload)
		ok := st == 422
		if _, leak := errBody["rows"]; leak {
			ok = false
		}
		if _, leak := errBody["exactLevel"]; leak {
			ok = false
		}
		fields, _ := errBody["fields"].([]any)
		found := false
		for _, f := range fields {
			if fm, isMap := f.(map[string]any); isMap && fm["field"] == wantField {
				found = true
			}
		}
		ok = ok && found
		check(name, ok, "status:", st, "wantField:", wantField, string(rb))
	}

	expect422("缺一行（5 行）", map[string]any{"bands": valid[:5]}, "bands")
	seven := append(bands([]float64{90, 92, 95.5, 100, 98, 94}, []float64{10, 12, 15.5, 20, 18, 14}),
		band{8000, 90, 10})
	expect422("额外频带（7 行）", map[string]any{"bands": seven}, "bands")

	badRange := bands([]float64{90, 141, 95.5, 100, 98, 94}, []float64{10, 12, 15.5, 20, 18, 14})
	expect422("L 越界（250 Hz = 141）", map[string]any{"bands": badRange}, "level")

	badAtt := bands([]float64{90, 92, 95.5, 100, 98, 94}, []float64{10, 40.5, 15.5, 20, 18, 14})
	expect422("A 越界（250 Hz = 40.5）", map[string]any{"bands": badAtt}, "attenuation")

	// 两位小数用裸报文构造
	rawTwoDec := `{"bands":[
		{"frequency":125,"level":90,"attenuation":10},
		{"frequency":250,"level":92,"attenuation":12},
		{"frequency":500,"level":95.55,"attenuation":15.5},
		{"frequency":1000,"level":100,"attenuation":20},
		{"frequency":2000,"level":98,"attenuation":18},
		{"frequency":4000,"level":94,"attenuation":14}]}`
	st, errBody, rb := postRawJSON(apiBase+"/api/calculate", rawTwoDec)
	check("两位小数（500 Hz = 95.55）返回 422 且定位 level",
		st == 422 && hasField(errBody, "level"), "status:", st, string(rb))

	rawInf := `{"bands":[
		{"frequency":125,"level":1e999,"attenuation":10},
		{"frequency":250,"level":92,"attenuation":12},
		{"frequency":500,"level":95.5,"attenuation":15.5},
		{"frequency":1000,"level":100,"attenuation":20},
		{"frequency":2000,"level":98,"attenuation":18},
		{"frequency":4000,"level":94,"attenuation":14}]}`
	st, errBody, rb = postRawJSON(apiBase+"/api/calculate", rawInf)
	check("无穷值 1e999 返回 422 且定位 level",
		st == 422 && hasField(errBody, "level"), "status:", st, string(rb))

	// 4) Web 前端容器（nginx）
	fmt.Println("[4] Vue 前端容器（nginx 托管 + 同源 /api 代理）")
	wresp, err := client.Get(webBase + "/")
	if err != nil {
		fmt.Printf("  ✗ 无法连接前端 %s: %v\n", webBase, err)
		os.Exit(1)
	}
	wbody, _ := io.ReadAll(wresp.Body)
	wresp.Body.Close()
	check("GET / 返回 200", wresp.StatusCode == 200, "status:", wresp.StatusCode)
	check("首页 HTML 引用了打包后的前端资源",
		strings.Contains(string(wbody), `<div id="app"></div>`) && strings.Contains(string(wbody), `/assets/`),
		"html snippet:", string(wbody))

	// 经 nginx 同源代理调用真实 API，结果必须与直连 API 完全一致
	st2, viaWeb, rb2 := postJSON(webBase+"/api/calculate", map[string]any{"bands": valid})
	check("经前端 nginx 代理调用 /api/calculate 返回 200", st2 == 200, "status:", st2, string(rb2))
	exact2, ok2 := viaWeb["exactLevel"].(float64)
	disp2, _ := viaWeb["displayLevel"].(string)
	check("代理链路与直连 API 结果一致", ok2 && math.Abs(exact2-exact) == 0 && disp2 == display,
		"direct:", exact, display, "via-web:", exact2, disp2)

	fmt.Printf("\n验收结果：%d 通过，%d 失败\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
	fmt.Println("🎉 全部验收通过")
}

func postRawJSON(url, raw string) (int, map[string]any, []byte) {
	resp, err := client.Post(url, "application/json", strings.NewReader(raw))
	if err != nil {
		failed++
		fmt.Printf("  ✗ 请求失败 %s: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(rb, &parsed)
	return resp.StatusCode, parsed, rb
}

func hasField(body map[string]any, field string) bool {
	fields, _ := body["fields"].([]any)
	for _, f := range fields {
		if fm, ok := f.(map[string]any); ok && fm["field"] == field {
			return true
		}
	}
	return false
}
