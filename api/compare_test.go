package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// compareBands 仅带频带与衰减，六个现场声级由顶层 levels 复用。
func compareBands(as []float64) []map[string]any {
	fs := []int64{125, 250, 500, 1000, 2000, 4000}
	rows := make([]map[string]any, 6)
	for i, f := range fs {
		rows[i] = map[string]any{"frequency": f, "attenuation": as[i]}
	}
	return rows
}

func compareBody(levels []any, aAtt, bAtt []float64) map[string]any {
	return map[string]any{
		"levels": levels,
		"candidates": []map[string]any{
			{"label": "甲", "bands": compareBands(aAtt)},
			{"label": "乙", "bands": compareBands(bAtt)},
		},
	}
}

func defaultLevels() []any {
	return []any{90.0, 92.0, 95.5, 100.0, 98.0, 94.0}
}

func doCompare(t *testing.T, payload any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleCompare(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v; body=%s", err, rec.Body.String())
	}
	return rec.Code, out
}

// findCandidate 在响应中按 label 取候选结果。
func findCandidate(t *testing.T, out map[string]any, label string) map[string]any {
	t.Helper()
	cands, ok := out["candidates"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("candidates 应当有两份结果，实际=%v", out["candidates"])
	}
	for _, c := range cands {
		cm := c.(map[string]any)
		if cm["label"] == label {
			return cm
		}
	}
	t.Fatalf("未找到候选 %s，实际=%v", label, out["candidates"])
	return nil
}

func TestHTTPCompareSameAttenuationIsTie(t *testing.T) {
	// 相同衰减 → 未舍入合成声级严格相等 → tie，结论说明效果相同。
	att := []float64{10, 12, 15.5, 20, 18, 14}
	status, out := doCompare(t, compareBody(defaultLevels(), att, att))
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200；响应=%v", status, out)
	}
	if out["tie"] != true {
		t.Fatalf("tie 应为 true，实际=%v；响应=%v", out["tie"], out)
	}
	if w := out["winner"]; w != "" {
		t.Fatalf("效果相同时 winner 应为空串，实际=%v", w)
	}
	conclusion, _ := out["conclusion"].(string)
	if conclusion == "" || !containsAll(conclusion, "相同") {
		t.Fatalf("结论文案应说明效果相同，实际=%q", conclusion)
	}

	ca := findCandidate(t, out, "甲")
	cb := findCandidate(t, out, "乙")
	// 两份完整结果均存在，且未舍入值严格一致（同一份衰减 + 同一组声级）。
	ea, eb := ca["exactLevel"].(float64), cb["exactLevel"].(float64)
	if ea != eb {
		t.Fatalf("相同衰减下未舍入值必须严格相等：%v vs %v", ea, eb)
	}
	if ca["displayLevel"] != cb["displayLevel"] {
		t.Fatalf("显示值应一致：%v vs %v", ca["displayLevel"], cb["displayLevel"])
	}
	// 每份结果都含六行逐带修正值、代入依据与公式。
	for _, cm := range []map[string]any{ca, cb} {
		rows := cm["rows"].([]any)
		if len(rows) != 6 {
			t.Fatalf("每个候选应返回 6 行，实际=%d", len(rows))
		}
		first := rows[0].(map[string]any)
		if first["corrected"].(float64) != 80.0 {
			t.Fatalf("甲首行 C 应为 80.0，实际=%v", first["corrected"])
		}
		if s, _ := cm["substitution"].(string); s == "" {
			t.Fatal("substitution 不能为空")
		}
		if s, _ := cm["formula"].(string); s == "" {
			t.Fatal("formula 不能为空")
		}
	}
}

func TestHTTPCompareClearHighLowPicksLower(t *testing.T) {
	// 乙在每个频带衰减都更大 5 dB → 乙佩戴后声级更低，乙更优。
	base := []float64{10, 12, 15.5, 20, 18, 14}
	better := []float64{15, 17, 20.5, 25, 23, 19}
	status, out := doCompare(t, compareBody(defaultLevels(), base, better))
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200；响应=%v", status, out)
	}
	if out["tie"] != false {
		t.Fatalf("tie 应为 false，实际=%v", out["tie"])
	}
	if out["winner"] != "乙" {
		t.Fatalf("winner 应为 乙，实际=%v", out["winner"])
	}
	conclusion, _ := out["conclusion"].(string)
	if !containsAll(conclusion, "乙", "更优") {
		t.Fatalf("结论文案应指出乙更优，实际=%q", conclusion)
	}

	ca := findCandidate(t, out, "甲")
	cb := findCandidate(t, out, "乙")
	ea, eb := ca["exactLevel"].(float64), cb["exactLevel"].(float64)
	if !(eb < ea) {
		t.Fatalf("乙未舍入值应更低：甲=%v 乙=%v", ea, eb)
	}
	// 每条频带乙的 C 恰好低 5 dB，整体合成声级也恰好低 5 dB。
	if d := math.Abs((ea - eb) - 5.0); d > 1e-9 {
		t.Fatalf("乙整体应低约 5 dB，实际差=%v", ea-eb)
	}
	// 交叉验证：显示值与各自未舍入值的十进制一位舍入一致。
	for _, cm := range []map[string]any{ca, cb} {
		exact := cm["exactLevel"].(float64)
		if cm["displayLevel"] != roundTenthsString(exact) {
			t.Fatalf("显示值与未舍入值舍入不一致：%v vs %q", exact, cm["displayLevel"])
		}
	}
}

func TestHTTPCompareReversedOrderStillPicksByLabel(t *testing.T) {
	// 候选顺序调换但 label 不变，判定仍按 label 给出（甲衰减更大 → 甲更优）。
	bigger := []float64{15, 17, 20.5, 25, 23, 19}
	base := []float64{10, 12, 15.5, 20, 18, 14}
	payload := map[string]any{
		"levels": defaultLevels(),
		"candidates": []map[string]any{
			{"label": "乙", "bands": compareBands(base)},
			{"label": "甲", "bands": compareBands(bigger)},
		},
	}
	status, out := doCompare(t, payload)
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200；响应=%v", status, out)
	}
	if out["winner"] != "甲" {
		t.Fatalf("winner 应为 甲，实际=%v", out["winner"])
	}
}

func TestHTTPCompare422LocalizesCandidateBandField(t *testing.T) {
	base := []float64{10, 12, 15.5, 20, 18, 14}

	cases := []struct {
		name      string
		mutate    func(body map[string]any)
		raw       string
		candidate string
		field     string
		frequency int
	}{
		{
			name: "乙缺一行",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				cands[1]["bands"] = compareBands(base)[:5]
			},
			candidate: "乙",
			field:     "bands",
		},
		{
			name: "甲 250Hz 衰减越界",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				rows := cands[0]["bands"].([]map[string]any)
				rows[1]["attenuation"] = 40.5
			},
			candidate: "甲",
			field:     "attenuation",
			frequency: 250,
		},
		{
			name: "乙 1000Hz 衰减为字符串",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				rows := cands[1]["bands"].([]map[string]any)
				rows[3]["attenuation"] = "20"
			},
			candidate: "乙",
			field:     "attenuation",
			frequency: 1000,
		},
		{
			name: "甲缺少衰减字段",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				rows := cands[0]["bands"].([]map[string]any)
				delete(rows[0], "attenuation")
			},
			candidate: "甲",
			field:     "attenuation",
			frequency: 125,
		},
		{
			name: "顶层现场声级越界（不归属任何候选）",
			mutate: func(b map[string]any) {
				lv := b["levels"].([]any)
				lv[0] = 141.0
			},
			candidate: "",
			field:     "level",
			frequency: 125,
		},
		{
			name:      "顶层声级为无穷值",
			raw:       compareRawWithInfLevel(),
			candidate: "",
			field:     "level",
			frequency: 125,
		},
		{
			name: "候选 label 非法",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				cands[0]["label"] = "丙"
			},
			candidate: "",
			field:     "candidates",
		},
		{
			name: "只有一个候选",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				b["candidates"] = cands[:1]
			},
			candidate: "",
			field:     "candidates",
		},
		{
			name: "甲频带顺序错误",
			mutate: func(b map[string]any) {
				cands := b["candidates"].([]map[string]any)
				rows := cands[0]["bands"].([]map[string]any)
				rows[0]["frequency"] = int64(250)
			},
			candidate: "甲",
			field:     "frequency",
			frequency: 125,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.raw != "" {
				req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewBufferString(tc.raw))
				rec = httptest.NewRecorder()
				handleCompare(rec, req)
			} else {
				payload := compareBody(defaultLevels(), base, base)
				tc.mutate(payload)
				raw, _ := json.Marshal(payload)
				req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(raw))
				rec = httptest.NewRecorder()
				handleCompare(rec, req)
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("状态码 = %d，期望 422；body=%s", rec.Code, rec.Body.String())
			}
			var out map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("响应不是合法 JSON: %v", err)
			}
			// 绝不泄漏任一候选的结果。
			if _, leak := out["candidates"]; leak {
				t.Fatal("422 响应不得包含任何候选结果 candidates")
			}
			fields, ok := out["fields"].([]any)
			if !ok || len(fields) == 0 {
				t.Fatalf("422 必须带字段定位，实际=%v", out)
			}
			found := false
			for _, f := range fields {
				fm := f.(map[string]any)
				if fm["field"] != tc.field {
					continue
				}
				if tc.candidate != "" && fm["candidate"] != tc.candidate {
					continue
				}
				if tc.frequency != 0 && int(fm["frequency"].(float64)) != tc.frequency {
					continue
				}
				found = true
			}
			if !found {
				t.Fatalf("未找到定位 candidate=%q field=%q frequency=%d；实际=%v",
					tc.candidate, tc.field, tc.frequency, fields)
			}
		})
	}
}

// 未舍入值有明确高低、但一位小数显示相同：结论仍须按未舍入值判定，且不得出现“低 0.0 dB”。
func TestHTTPCompareDecidesByExactValueWhenDisplaysEqual(t *testing.T) {
	// 125/250 Hz 两个等高主导带：乙仅在 125 Hz 多衰减 0.1 dB，
	// 总合成声级只下降约 0.05 dB（0.0497），两位显示值同为 83.0，但乙未舍入值更低。
	levels := []any{100.0, 100.0, 40.0, 40.0, 40.0, 40.0}
	aAtt := []float64{20, 20, 0, 0, 0, 0}
	bAtt := []float64{20.1, 20, 0, 0, 0, 0}
	status, out := doCompare(t, compareBody(levels, aAtt, bAtt))
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；响应=%v", status, out)
	}
	if out["winner"] != "乙" || out["tie"] != false {
		t.Fatalf("应按未舍入值判定乙更优，实际 winner=%v tie=%v", out["winner"], out["tie"])
	}
	ca := findCandidate(t, out, "甲")
	cb := findCandidate(t, out, "乙")
	ea, eb := ca["exactLevel"].(float64), cb["exactLevel"].(float64)
	if eb >= ea {
		t.Fatal("乙未舍入值应更低")
	}
	if d := ea - eb; d >= 0.05 || d <= 0.0 {
		t.Fatalf("未舍入差值应在 (0, 0.05) 内，实际=%v", d)
	}
	if ca["displayLevel"] != cb["displayLevel"] {
		t.Fatalf("本用例两位显示值应相同，实际 %v / %v", ca["displayLevel"], cb["displayLevel"])
	}
	if ca["displayLevel"] != "83.0" {
		t.Fatalf("显示值应为 83.0，实际 %v", ca["displayLevel"])
	}
	conclusion, _ := out["conclusion"].(string)
	if containsAll(conclusion, "低 0.0 dB") {
		t.Fatalf("结论不得出现“低 0.0 dB”，实际=%q", conclusion)
	}
	if !containsAll(conclusion, "乙", "未舍入") {
		t.Fatalf("结论应说明按未舍入值乙更低，实际=%q", conclusion)
	}
}

func TestHTTPCompareMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/compare", nil)
	rec := httptest.NewRecorder()
	handleCompare(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d，期望 405", rec.Code)
	}
}

// 与单耳罩端点交叉验证：对比中每个候选的结果必须等于 /api/calculate 的结果。
func TestHTTPCompareMatchesSingleCalc(t *testing.T) {
	levels := defaultLevels()
	aAtt := []float64{10, 12, 15.5, 20, 18, 14}
	bAtt := []float64{15, 17, 20.5, 25, 23, 19}
	status, out := doCompare(t, compareBody(levels, aAtt, bAtt))
	if status != 200 {
		t.Fatalf("对比请求失败: %v", out)
	}
	for label, att := range map[string][]float64{"甲": aAtt, "乙": bAtt} {
		bands := make([]map[string]any, 6)
		for i := range levels {
			bands[i] = map[string]any{
				"frequency":   []int64{125, 250, 500, 1000, 2000, 4000}[i],
				"level":       levels[i],
				"attenuation": att[i],
			}
		}
		singleStatus, single := doCalc(t, jsonString(bands))
		if singleStatus != 200 {
			t.Fatalf("单耳罩请求失败: %v", single)
		}
		cmp := findCandidate(t, out, label)
		if cmp["exactLevel"] != single["exactLevel"] {
			t.Fatalf("候选 %s 的未舍入值 %v 与单耳罩 %v 不一致",
				label, cmp["exactLevel"], single["exactLevel"])
		}
		if cmp["displayLevel"] != single["displayLevel"] {
			t.Fatalf("候选 %s 显示值 %v 与单耳罩 %v 不一致",
				label, cmp["displayLevel"], single["displayLevel"])
		}
		if cmp["substitution"] != single["substitution"] {
			t.Fatalf("候选 %s 代入式与单耳罩不一致", label)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}

func jsonString(bands []map[string]any) string {
	raw, _ := json.Marshal(map[string]any{"bands": bands})
	return string(raw)
}

func compareRawWithInfLevel() string {
	return fmt.Sprintf(`{"levels":[1e999,92,95.5,100,98,94],"candidates":[
		{"label":"甲","bands":%s},
		{"label":"乙","bands":%s}]}`,
		jsonAttenuationArray(), jsonAttenuationArray())
}

func jsonAttenuationArray() string {
	fs := []int{125, 250, 500, 1000, 2000, 4000}
	as := []float64{10, 12, 15.5, 20, 18, 14}
	var b bytes.Buffer
	b.WriteByte('[')
	for i := range fs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"frequency":%d,"attenuation":%v}`, fs[i], as[i])
	}
	b.WriteByte(']')
	return b.String()
}
