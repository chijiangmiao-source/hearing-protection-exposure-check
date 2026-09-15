package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// bandInput 是一行倍频带读数。RawMessage 保留每个字段的原始 JSON 词法单元，
// 缺填（nil）与类型错误都能精确定位到具体字段。
type bandInput struct {
	Frequency   json.RawMessage `json:"frequency"`
	Level       json.RawMessage `json:"level"`
	Attenuation json.RawMessage `json:"attenuation"`
}

// calcRequest 是整单请求。Bands 用指针以便识别缺失的 bands 字段。
type calcRequest struct {
	Bands *[]bandInput `json:"bands"`
}

// validate 解析并校验整单：六个固定频带、缺一补一不可、频带不得增减，
// 任何一行不合法都整单拒绝（收集全部定位错误，但绝不产生任何结果）。
func validate(raw []byte) (calcRequest, []fieldError) {
	var req calcRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, []fieldError{{Field: "bands", Message: "请求体不是合法的 JSON 对象：" + cleanJSONErr(err)}}
	}
	// 请求体里不允许出现第二个 JSON 值。
	if dec.More() {
		return req, []fieldError{{Field: "bands", Message: "请求体中存在多余的 JSON 数据"}}
	}

	if req.Bands == nil {
		return req, []fieldError{{Field: "bands", Message: "缺少 bands 字段"}}
	}
	bands := *req.Bands
	if len(bands) != len(frequencies) {
		return req, []fieldError{{
			Field:   "bands",
			Message: fmt.Sprintf("必须恰好提交 %d 个倍频带（125–4000 Hz），实际收到 %d 行", len(frequencies), len(bands)),
		}}
	}

	var errs []fieldError
	for i, row := range bands {
		f := frequencies[i]

		// 频带必须与固定顺序逐行一致（整数字面量，125.0 也不接受）。
		if n, err := strconv.ParseInt(string(row.Frequency), 10, 64); err != nil || int(n) != f {
			got := "缺失"
			if len(row.Frequency) > 0 {
				got = string(row.Frequency)
			}
			errs = append(errs, fieldError{
				Field: "frequency", Frequency: f,
				Message: fmt.Sprintf("第 %d 行频带必须是 %d Hz，收到 %s", i+1, f, got),
			})
		}

		errs = append(errs, checkNumber("level", f, row.Level, levelMin, levelMax, "现场声级 L")...)
		errs = append(errs, checkNumber("attenuation", f, row.Attenuation, attMin, attMax, "耳罩衰减 A")...)
	}
	return req, errs
}

// checkNumber 负责单个数值字段的缺填、类型、NaN/无穷、越界与小数位校验。
func checkNumber(field string, f int, raw json.RawMessage, min, max float64, label string) []fieldError {
	fe := func(msg string) fieldError {
		return fieldError{Field: field, Frequency: f, Message: msg}
	}
	if len(raw) == 0 {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 缺少数值", f, label))}
	}
	s := string(raw)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 不是数值：%s", f, label, s))}
	}
	if math.IsNaN(v) {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 不允许为 NaN", f, label))}
	}
	if math.IsInf(v, 0) {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 不允许为无穷值", f, label))}
	}
	if v < min || v > max {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 必须在 %.1f 至 %.1f dB 之间，当前为 %s", f, label, min, max, s))}
	}
	if !isTenth(v) {
		return []fieldError{fe(fmt.Sprintf("%d Hz 的 %s 只允许一位小数，当前为 %s", f, label, s))}
	}
	return nil
}

func cleanJSONErr(err error) string {
	if err.Error() == "EOF" {
		return "请求体为空"
	}
	return err.Error()
}
