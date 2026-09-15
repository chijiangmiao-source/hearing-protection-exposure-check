package main

import (
	"fmt"
	"math"
	"math/big"
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

// fieldError 定位到具体字段，供前端高亮与聚焦。
type fieldError struct {
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
