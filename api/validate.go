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

// candidateInput 是对比请求中的一个候选耳罩。候选只录入逐带衰减值，
// 六个现场声级由对比请求顶层的 levels 统一提供并复用。
type candidateInput struct {
	Label string       `json:"label"`
	Bands *[]bandInput `json:"bands"`
}

// compareRequest 是对比整单：一组现场声级 + 甲、乙两份逐带衰减。
type compareRequest struct {
	Levels     *[]json.RawMessage `json:"levels"`
	Candidates *[]candidateInput  `json:"candidates"`
}

// parseObject 用与既有整单一致的严格方式解析 JSON：拒绝未知字段，且只允许单个 JSON 值。
// 解析阶段的错误无法定位到具体字段时，以 rootField（"bands"/"levels"/"candidates"）上报。
func parseObject(raw []byte, v any, rootField string) []fieldError {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return []fieldError{{Field: rootField, Message: "请求体不是合法的 JSON 对象：" + cleanJSONErr(err)}}
	}
	if dec.More() {
		return []fieldError{{Field: rootField, Message: "请求体中存在多余的 JSON 数据"}}
	}
	return nil
}

// validate 解析并校验整单：六个固定频带、缺一补一不可、频带不得增减，
// 任何一行不合法都整单拒绝（收集全部定位错误，但绝不产生任何结果）。
func validate(raw []byte) (calcRequest, []fieldError) {
	var req calcRequest
	if errs := parseObject(raw, &req, "bands"); len(errs) > 0 {
		return req, errs
	}
	if req.Bands == nil {
		return req, []fieldError{{Field: "bands", Message: "缺少 bands 字段"}}
	}
	_, errs := validateBands(*req.Bands, nil)
	return req, errs
}

// validateBands 校验六行固定频带。levels 非空时，每行的现场声级 L 改用
// levels[i] 的原始词法单元（对比端点复用同组声级，候选行不再单独提交 level）。
// 返回的 bandInput 已按固定顺序补齐 Level，供后续计算直接使用。
func validateBands(bands []bandInput, levels []json.RawMessage) ([]bandInput, []fieldError) {
	if len(bands) != len(frequencies) {
		return bands, []fieldError{{
			Field:   "bands",
			Message: fmt.Sprintf("必须恰好提交 %d 个倍频带（125–4000 Hz），实际收到 %d 行", len(frequencies), len(bands)),
		}}
	}

	out := make([]bandInput, len(bands))
	var errs []fieldError
	for i, row := range bands {
		f := frequencies[i]
		if levels != nil {
			row.Level = levels[i]
		}
		out[i] = row

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
	return out, errs
}

// validateLevels 校验对比请求顶层复用的六个现场声级，顺序与固定频带一致。
func validateLevels(raws []json.RawMessage) []fieldError {
	if len(raws) != len(frequencies) {
		return []fieldError{{
			Field: "levels",
			Message: fmt.Sprintf("必须恰好提交 %d 个现场声级（125–4000 Hz），实际收到 %d 个",
				len(frequencies), len(raws)),
		}}
	}
	var errs []fieldError
	for i, raw := range raws {
		errs = append(errs, checkNumber("level", frequencies[i], raw, levelMin, levelMax, "现场声级 L")...)
	}
	return errs
}

// validateCandidates 校验甲、乙两个候选耳罩：候选必须恰好两个、标识合法，
// 且各自的六行逐带衰减全部通过。任何错误都附加候选标识后整单返回。
func validateCandidates(candidates []candidateInput, levels []json.RawMessage) (map[string][]bandInput, []fieldError) {
	parsed := make(map[string][]bandInput)
	if len(candidates) != 2 {
		return parsed, []fieldError{{
			Field:   "candidates",
			Message: fmt.Sprintf("必须恰好提交甲、乙两个候选耳罩，实际收到 %d 个", len(candidates)),
		}}
	}

	var errs []fieldError
	seen := make(map[string]bool)
	for ci, cand := range candidates {
		label := cand.Label
		if !isCandidateLabel(label) {
			errs = append(errs, fieldError{
				Field:   "candidates",
				Message: fmt.Sprintf("第 %d 个候选的 label 必须是 %q 或 %q，收到 %q", ci+1, candidateA, candidateB, label),
			})
			continue
		}
		if seen[label] {
			errs = append(errs, fieldError{
				Field:   "candidates",
				Message: fmt.Sprintf("候选耳罩 %s 重复提交，甲、乙各需一份", label),
			})
			continue
		}
		seen[label] = true
		if cand.Bands == nil {
			errs = append(errs, fieldError{
				Candidate: label, Field: "bands",
				Message: fmt.Sprintf("候选耳罩 %s 缺少 bands 字段", label),
			})
			continue
		}
		rows, rowErrs := validateBands(*cand.Bands, levels)
		for _, e := range rowErrs {
			// level 的错误归属顶层声级，候选标识只标注候选自身字段；
			// 这里候选行复用了顶层 levels，level 错误已由 validateLevels 统一上报，跳过。
			if e.Field == "level" {
				continue
			}
			e.Candidate = label
			errs = append(errs, e)
		}
		parsed[label] = rows
	}
	return parsed, errs
}

// validateCompare 校验对比整单：levels 与 candidates 同时存在、各自合法，
// 任何一处错误都整单拒绝（收集全部定位，但绝不产生任何结果）。
// 返回按候选标识索引的六行（已把顶层声级注入每行），供计算阶段直接使用。
func validateCompare(raw []byte) (compareRequest, map[string][]bandInput, []fieldError) {
	var req compareRequest
	if errs := parseObject(raw, &req, "candidates"); len(errs) > 0 {
		return req, nil, errs
	}

	var errs []fieldError
	if req.Levels == nil {
		errs = append(errs, fieldError{Field: "levels", Message: "缺少 levels 字段"})
	}
	if req.Candidates == nil {
		errs = append(errs, fieldError{Field: "candidates", Message: "缺少 candidates 字段"})
	}
	if errs != nil {
		return req, nil, errs
	}

	levels := *req.Levels
	errs = append(errs, validateLevels(levels)...)

	// 即便声级有问题也继续把六个词法单元准备好，供候选行复用，避免二次空指针。
	safeLevels := levels
	if len(safeLevels) != len(frequencies) {
		safeLevels = make([]json.RawMessage, len(frequencies))
		copy(safeLevels, levels)
	}

	parsed, candErrs := validateCandidates(*req.Candidates, safeLevels)
	errs = append(errs, candErrs...)
	return req, parsed, errs
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
