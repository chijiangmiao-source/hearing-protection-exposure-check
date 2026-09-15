package main

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCombineLevelsAllEqual(t *testing.T) {
	// 六个相同的 C=70.0：E = 70 + 10·log10(6) = 77.7815125038… dB
	cs := []float64{70, 70, 70, 70, 70, 70}
	got, err := combineLevels(cs)
	if err != nil {
		t.Fatalf("combineLevels 返回错误: %v", err)
	}
	want := 70 + 10*math.Log10(6)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("combineLevels = %v, 期望 %v", got, want)
	}
	if d := roundTenthsString(got); d != "77.8" {
		t.Fatalf("显示值 = %q, 期望 \"77.8\"", d)
	}
}

func TestCombineLevelsDominantBand(t *testing.T) {
	// 一条 100 dB 主带加一条 90 dB：能量叠加后 ≈ 100.4139 dB。
	cs := []float64{100, 90, 0, 0, 0, 0}
	got, err := combineLevels(cs)
	if err != nil {
		t.Fatalf("combineLevels 返回错误: %v", err)
	}
	want := 10 * math.Log10(math.Pow10(10)+math.Pow10(9)+4)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("combineLevels = %v, 期望 %v", got, want)
	}
	if d := roundTenthsString(got); d != "100.4" {
		t.Fatalf("显示值 = %q, 期望 \"100.4\"", d)
	}
}

func TestCombineLevelsRejectsNonFinite(t *testing.T) {
	for _, cs := range [][]float64{{math.NaN()}, {math.Inf(1)}, {math.Inf(-1)}} {
		if _, err := combineLevels(cs); err == nil {
			t.Fatalf("combineLevels(%v) 应当返回错误", cs)
		}
	}
}

func TestRoundTenthsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{77.75, "77.8"}, // 恰为 x.x5（77.75 在二进制中精确），必须进位
		{0.55, "0.6"},   // 浮点值 0.5500000000000000444…，高于半格，进位
		{77.85, "77.8"}, // 字面量 77.85 的实际浮点值为 77.84999…，舍入为 77.8
		{77.74, "77.7"}, // 不足半格，舍去
		{77.04, "77.0"}, // 避免浮点误差造成假进位
		{76.95, "77.0"}, // 跨整数进位
		{0.05, "0.1"},   // 半格进位
		{-0.05, "-0.1"}, // 负数向远离零方向
		{82.7815125, "82.8"},
		{40.0, "40.0"},
		{140.0, "140.0"},
		{0, "0.0"},
	}
	for _, c := range cases {
		if got := roundTenthsString(c.in); got != c.want {
			t.Errorf("roundTenthsString(%v) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestIsTenth(t *testing.T) {
	good := []float64{40, 40.1, 140.0, 99.9, 0, 40, 105.3}
	bad := []float64{40.15, 90.01, 140.001, 40.99}
	for _, v := range good {
		if !isTenth(v) {
			t.Errorf("isTenth(%v) 应当为 true", v)
		}
	}
	for _, v := range bad {
		if isTenth(v) {
			t.Errorf("isTenth(%v) 应当为 false", v)
		}
	}
}

func TestBuildSubstitution(t *testing.T) {
	got := buildSubstitution([]float64{70, 65.5, 0, 0, 0, 80})
	want := "E = 10 × log10( 10^(70.0/10) + 10^(65.5/10) + 10^(0.0/10) + 10^(0.0/10) + 10^(0.0/10) + 10^(80.0/10) )"
	if got != want {
		t.Fatalf("代入式 = %q, 期望 %q", got, want)
	}
}

// 便于测试构造请求体。
func bandJSON(f int64, l, a any) map[string]any {
	return map[string]any{"frequency": f, "level": l, "attenuation": a}
}

func validBands() []map[string]any {
	fs := []int64{125, 250, 500, 1000, 2000, 4000}
	rows := make([]map[string]any, 0, 6)
	for _, f := range fs {
		rows = append(rows, bandJSON(f, 85.0, 10.0))
	}
	return rows
}

func TestValidationErrorCases(t *testing.T) {
	base := validBands()

	cases := []struct {
		name   string
		mutate func(bands []map[string]any) any
		raw    string // 非空时直接作为请求体（用于无法用 float64 表示的 1e999 等）
		field  string // 至少应出现的定位字段
	}{
		{
			name: "缺一行",
			mutate: func(b []map[string]any) any {
				return map[string]any{"bands": b[:5]}
			},
			field: "bands",
		},
		{
			name: "额外频带",
			mutate: func(b []map[string]any) any {
				extra := append(b, bandJSON(8000, 85, 10))
				return map[string]any{"bands": extra}
			},
			field: "bands",
		},
		{
			name: "频带顺序错误",
			mutate: func(b []map[string]any) any {
				b[0]["frequency"] = int64(250)
				return map[string]any{"bands": b}
			},
			field: "frequency",
		},
		{
			name: "L 越上界",
			mutate: func(b []map[string]any) any {
				b[2]["level"] = 140.1
				return map[string]any{"bands": b}
			},
			field: "level",
		},
		{
			name: "L 越下界",
			mutate: func(b []map[string]any) any {
				b[0]["level"] = 39.9
				return map[string]any{"bands": b}
			},
			field: "level",
		},
		{
			name: "A 越界",
			mutate: func(b []map[string]any) any {
				b[1]["attenuation"] = 40.1
				return map[string]any{"bands": b}
			},
			field: "attenuation",
		},
		{
			name: "A 为负",
			mutate: func(b []map[string]any) any {
				b[1]["attenuation"] = -0.1
				return map[string]any{"bands": b}
			},
			field: "attenuation",
		},
		{
			name: "两位小数",
			mutate: func(b []map[string]any) any {
				b[3]["level"] = 85.15
				return map[string]any{"bands": b}
			},
			field: "level",
		},
		{
			name: "缺 L 字段",
			mutate: func(b []map[string]any) any {
				delete(b[4], "level")
				return map[string]any{"bands": b}
			},
			field: "level",
		},
		{
			name: "L 为字符串",
			mutate: func(b []map[string]any) any {
				b[0]["level"] = "85"
				return map[string]any{"bands": b}
			},
			field: "level",
		},
		{
			name: "L 为 1e999（无穷值）",
			raw: `{"bands":[
				{"frequency":125,"level":1e999,"attenuation":10},
				{"frequency":250,"level":85,"attenuation":10},
				{"frequency":500,"level":85,"attenuation":10},
				{"frequency":1000,"level":85,"attenuation":10},
				{"frequency":2000,"level":85,"attenuation":10},
				{"frequency":4000,"level":85,"attenuation":10}]}`,
			field: "level",
		},
		{
			name: "额外的顶层字段",
			mutate: func(b []map[string]any) any {
				return map[string]any{"bands": b, "extra": 1}
			},
			field: "bands",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			if tc.raw != "" {
				body = []byte(tc.raw)
			} else {
				payload := tc.mutate(cloneBands(base))
				body, _ = json.Marshal(payload)
			}
			_, errs := validate(body)
			if len(errs) == 0 {
				t.Fatalf("期望校验失败，但通过了；payload=%s", body)
			}
			found := false
			for _, fe := range errs {
				if fe.Field == tc.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("错误定位中没有字段 %q，实际=%+v", tc.field, errs)
			}
		})
	}
}

func TestNaNRejectedDirectly(t *testing.T) {
	if errs := checkNumber("level", 125, json.RawMessage("NaN"), 40, 140, "现场声级 L"); len(errs) == 0 {
		t.Fatal("NaN 必须被拒绝")
	}
	if errs := checkNumber("attenuation", 125, json.RawMessage("-1e999"), 0, 40, "耳罩衰减 A"); len(errs) == 0 {
		t.Fatal("无穷值必须被拒绝")
	}
}

func cloneBands(b []map[string]any) []map[string]any {
	out := make([]map[string]any, len(b))
	for i, m := range b {
		out[i] = map[string]any{}
		for k, v := range m {
			out[i][k] = v
		}
	}
	return out
}
