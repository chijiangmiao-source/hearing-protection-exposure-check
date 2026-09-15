package main

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// frequencies 是表单与 API 共同固定的六个倍频带中心频率（Hz），顺序即显示顺序。
var frequencies = []int{125, 250, 500, 1000, 2000, 4000}

const (
	levelMin = 40.0
	levelMax = 140.0
	attMin   = 0.0
	attMax   = 40.0
)

const formulaText = "E = 10 × log10( Σ 10^(C/10) )，其中 C = L − A"

// 对比端点固定的两个候选耳罩标识。
const (
	candidateA = "甲"
	candidateB = "乙"
)

func isCandidateLabel(s string) bool {
	return s == candidateA || s == candidateB
}

// fieldError 定位到具体字段，供前端高亮与聚焦。
// Candidate 仅对比端点使用（候选标识，如 "甲"/"乙"），单耳罩链路保持省略，
// 因此 POST /api/calculate 的错误响应结构与旧版逐字节兼容。
type fieldError struct {
	Candidate string `json:"candidate,omitempty"`
	Field     string `json:"field"`
	Frequency int    `json:"frequency,omitempty"`
	Message   string `json:"message"`
}

type errorResponse struct {
	Error  string       `json:"error"`
	Fields []fieldError `json:"fields,omitempty"`
}

type rowOutput struct {
	Frequency   int     `json:"frequency"`
	Level       float64 `json:"level"`
	Attenuation float64 `json:"attenuation"`
	Corrected   float64 `json:"corrected"`
}

// calcResponse 中的 ExactLevel 是未舍入的内部值，DisplayLevel 才是四舍五入后的显示值。
type calcResponse struct {
	Rows         []rowOutput `json:"rows"`
	Formula      string      `json:"formula"`
	Substitution string      `json:"substitution"`
	ExactLevel   float64     `json:"exactLevel"`
	DisplayLevel string      `json:"displayLevel"`
}

// compareCandidateResult 是单个候选耳罩的完整核算结果，结构与 calcResponse 完全一致，
// 以便前端并排复用同一份逐带修正值、代入依据、精确值与显示值。
type compareCandidateResult struct {
	Label        string      `json:"label"`
	Rows         []rowOutput `json:"rows"`
	Formula      string      `json:"formula"`
	Substitution string      `json:"substitution"`
	ExactLevel   float64     `json:"exactLevel"`
	DisplayLevel string      `json:"displayLevel"`
}

// compareResponse 是对比端点响应：甲、乙两份完整结果 + 仅依据未舍入合成声级得出的结论。
type compareResponse struct {
	Candidates []compareCandidateResult `json:"candidates"`
	// Winner 为 "甲"/"乙"；效果相同时为空串。
	Winner     string `json:"winner"`
	Tie        bool   `json:"tie"`
	Conclusion string `json:"conclusion"`
}

// computeResult 是单耳罩与对比端点共用的合成算法：逐带 C=L−A，再对数能量合成。
// 调用前各字段已经过整单校验，正常不会返回错误。
func computeResult(bands []bandInput) (calcResponse, error) {
	rows := make([]rowOutput, 0, len(bands))
	cs := make([]float64, 0, len(bands))
	for i, row := range bands {
		l, _ := strconv.ParseFloat(string(row.Level), 64)
		a, _ := strconv.ParseFloat(string(row.Attenuation), 64)
		// C = L − A。输入均为一位小数，结果在数学上也是一位小数，
		// 用 roundTenthsNumber 消除二进制减法误差。
		c := roundTenthsNumber(l - a)
		cs = append(cs, c)
		rows = append(rows, rowOutput{
			Frequency:   frequencies[i],
			Level:       l,
			Attenuation: a,
			Corrected:   c,
		})
	}

	exact, err := combineLevels(cs)
	if err != nil {
		return calcResponse{}, err
	}
	return calcResponse{
		Rows:         rows,
		Formula:      formulaText,
		Substitution: buildSubstitution(cs),
		ExactLevel:   exact,
		DisplayLevel: roundTenthsString(exact),
	}, nil
}

// decideWinner 仅依据未舍入合成声级判定：合成声级越低，耳罩降噪效果越好。
// 两个未舍入值在数值上严格相等时判为效果相同（Tie）。
func decideWinner(a, b compareCandidateResult) (winner string, tie bool) {
	switch {
	case a.ExactLevel < b.ExactLevel:
		return a.Label, false
	case a.ExactLevel > b.ExactLevel:
		return b.Label, false
	default:
		return "", true
	}
}

// compareConclusionText 生成给检测员直接展示的结论文案；页面只渲染该服务端文案，
// 不自行根据数值拼装。判定依据始终是未舍入值；差值通常按一位小数展示，
// 但若未舍入差值小于 0.05 dB（两款显示值相同而未舍入值仍有高低），则改用明确措辞，
// 避免出现“低 0.0 dB”的矛盾表述。
func compareConclusionText(a, b compareCandidateResult, winner string, tie bool) string {
	if tie {
		return fmt.Sprintf("甲、乙两款耳罩佩戴后的合成声级均为 %s dB，降噪效果相同", a.DisplayLevel)
	}
	diff := math.Abs(a.ExactLevel - b.ExactLevel)
	better, worse := a, b
	if winner == b.Label {
		better, worse = b, a
	}
	if better.DisplayLevel == worse.DisplayLevel {
		return fmt.Sprintf(
			"候选耳罩%s与%s佩戴后合成声级的一位小数显示值均为 %s dB，但按未舍入合成声级比较，%s 更低，降噪效果更优，建议选用%s",
			better.Label, worse.Label, better.DisplayLevel, better.Label, winner)
	}
	return fmt.Sprintf("候选耳罩%s的佩戴后合成声级更低（%s dB，%s 为 %s dB），低 %s dB，降噪效果更优，建议选用%s",
		winner, better.DisplayLevel, worse.Label, worse.DisplayLevel, roundTenthsString(diff), winner)
}

// combineLevels 实现 E = 10×log10(Σ10^(C/10))。
// 调用方必须保证 cs 非空且全部有限；本函数不做任何舍入。
func combineLevels(cs []float64) (float64, error) {
	sum := 0.0
	for _, c := range cs {
		if math.IsNaN(c) || math.IsInf(c, 0) {
			return 0, fmt.Errorf("band value is not finite")
		}
		sum += math.Pow(10, c/10.0)
	}
	if math.IsNaN(sum) || math.IsInf(sum, 0) || sum <= 0 {
		return 0, fmt.Errorf("power sum is not finite")
	}
	e := 10.0 * math.Log10(sum)
	if math.IsNaN(e) || math.IsInf(e, 0) {
		return 0, fmt.Errorf("combined level is not finite")
	}
	return e, nil
}

// roundTenthsNumber 仅用于把 C=L-A 规整回数学上的一位小数（消除浮点减法误差），
// 返回的仍是数值。最终显示值的舍入与格式化统一走 roundTenthsString。
func roundTenthsNumber(v float64) float64 {
	return math.Round(v*10.0) / 10.0
}

// roundTenthsString 按十进制四舍五入（遇 0.05 进位，round half away from zero），
// 返回恰好一位小数的字符串。计算基于 float64 的精确有理值，避免二进制尾数
// 造成的“假偶数/假进位”。
func roundTenthsString(v float64) string {
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		panic("roundTenthsString: value is not finite")
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(10, 1))
	num, den := scaled.Num(), scaled.Denom()
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int)) // 向零取整
	if rem.Sign() != 0 {
		twice := new(big.Int).Mul(new(big.Int).Abs(rem), big.NewInt(2))
		if twice.Cmp(den) >= 0 { // 余数 ≥ 1/2
			if num.Sign() >= 0 {
				q.Add(q, big.NewInt(1))
			} else {
				q.Sub(q, big.NewInt(1))
			}
		}
	}
	negative := q.Sign() < 0
	abs := new(big.Int).Abs(q)
	tenths := new(big.Int).Mod(abs, big.NewInt(10))
	whole := new(big.Int).Quo(abs, big.NewInt(10))
	s := whole.String() + "." + tenths.String()
	if negative {
		s = "-" + s
	}
	return s
}

// isTenth 判断数值最多只有一位小数（如 90、90.1），90.15 或 90.0001 均不合法。
func isTenth(v float64) bool {
	scaled := v * 10.0
	return math.Abs(scaled-math.Round(scaled)) < 1e-7
}

// buildSubstitution 生成公式代入依据，例如：
// E = 10 × log10( 10^(70.0/10) + 10^(65.0/10) + … + 10^(80.0/10) )
func buildSubstitution(cs []float64) string {
	var b strings.Builder
	b.WriteString("E = 10 × log10( ")
	for i, c := range cs {
		if i > 0 {
			b.WriteString(" + ")
		}
		fmt.Fprintf(&b, "10^(%s/10)", roundTenthsString(c))
	}
	b.WriteString(" )")
	return b.String()
}
