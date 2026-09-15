package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doCalc(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/calculate", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handleCalc(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v; body=%s", err, rec.Body.String())
	}
	return rec.Code, out
}

const validBody = `{"bands":[
	{"frequency":125,"level":90,"attenuation":10},
	{"frequency":250,"level":92,"attenuation":12},
	{"frequency":500,"level":95.5,"attenuation":15.5},
	{"frequency":1000,"level":100,"attenuation":20},
	{"frequency":2000,"level":98,"attenuation":18},
	{"frequency":4000,"level":94,"attenuation":14}]}`

func TestHTTPCalculateSuccess(t *testing.T) {
	status, out := doCalc(t, validBody)
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200；响应=%v", status, out)
	}
	rows, ok := out["rows"].([]any)
	if !ok || len(rows) != 6 {
		t.Fatalf("rows 应当有 6 行，实际=%v", out["rows"])
	}
	first := rows[0].(map[string]any)
	if first["corrected"].(float64) != 80.0 {
		t.Fatalf("第一行 C = %v, 期望 80", first["corrected"])
	}
	third := rows[2].(map[string]any)
	if third["corrected"].(float64) != 80.0 {
		t.Fatalf("第三行 C = %v, 期望 80（95.5−15.5）", third["corrected"])
	}
	if out["displayLevel"].(string) == "" {
		t.Fatal("displayLevel 不能为空")
	}
	if _, ok := out["exactLevel"].(float64); !ok {
		t.Fatal("exactLevel 必须是未舍入数值")
	}
	if _, ok := out["substitution"].(string); !ok {
		t.Fatal("substitution 必须是公式代入字符串")
	}

	// 交叉验证：显示值必须等于对未舍入值做一位小数四舍五入。
	exact := out["exactLevel"].(float64)
	if got, want := out["displayLevel"].(string), roundTenthsString(exact); got != want {
		t.Fatalf("显示值 %q 与未舍入值 %v 的舍入结果 %q 不一致", got, exact, want)
	}

	// 独立复算，确认 API 没有返回固定结果。
	var sum float64
	for _, r := range rows {
		c := r.(map[string]any)["corrected"].(float64)
		sum += math.Pow(10, c/10)
	}
	want := 10 * math.Log10(sum)
	if d := want - exact; d > 1e-9 || d < -1e-9 {
		t.Fatalf("exactLevel=%v 与独立复算 %v 不符", exact, want)
	}
}

func TestHTTPCalculateRejectsWholeOrder(t *testing.T) {
	// 第二行越界：整单 422，且响应里绝不允许出现 rows / exactLevel。
	bad := `{"bands":[
		{"frequency":125,"level":90,"attenuation":10},
		{"frequency":250,"level":141,"attenuation":12},
		{"frequency":500,"level":95,"attenuation":15},
		{"frequency":1000,"level":100,"attenuation":20},
		{"frequency":2000,"level":98,"attenuation":18},
		{"frequency":4000,"level":94,"attenuation":14}]}`
	status, out := doCalc(t, bad)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("状态码 = %d, 期望 422", status)
	}
	if _, ok := out["rows"]; ok {
		t.Fatal("422 响应不得包含任何部分结果 rows")
	}
	if _, ok := out["exactLevel"]; ok {
		t.Fatal("422 响应不得包含 exactLevel")
	}
	fields, ok := out["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("422 必须带字段定位信息，实际=%v", out)
	}
	fe := fields[0].(map[string]any)
	if fe["field"] != "level" || fe["frequency"].(float64) != 250 {
		t.Fatalf("定位错误，实际=%v", fe)
	}
}

func TestHTTPHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handleHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health 状态码 = %d", rec.Code)
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/calculate", nil)
	rec := httptest.NewRecorder()
	handleCalc(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405", rec.Code)
	}
}
