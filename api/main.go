package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleCalc 是唯一的计算端点：POST /api/calculate。
// 校验不通过整单返回 422，且不会产生任何部分结果。
func handleCalc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "只允许 POST 请求"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "请求体读取失败：" + err.Error()})
		return
	}

	req, errs := validate(body)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "整单校验未通过，请修正标红字段后重新提交", Fields: errs})
		return
	}

	bands := *req.Bands
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
		// 输入已限定在 40−140 / 0−40，C 落在 0−140，正常不会走到这里；
		// 一旦发生仍按整单失败处理，绝不返回部分结果。
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "合成声级计算失败：" + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, calcResponse{
		Rows:         rows,
		Formula:      formulaText,
		Substitution: buildSubstitution(cs),
		ExactLevel:   exact,
		DisplayLevel: roundTenthsString(exact),
	})
}

// handleHealth 供 Compose 健康检查与验收脚本使用。
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "只允许 GET 请求"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// withLogging 是最小化的请求日志中间件。
func withLogging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", withLogging(handleHealth))
	mux.HandleFunc("/api/calculate", withLogging(handleCalc))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "未找到：可用端点 GET /health、POST /api/calculate"})
	})

	addr := ":" + port()
	log.Printf("earmuff API listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: mux}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func port() string {
	if p := os.Getenv("API_PORT"); p != "" {
		return p
	}
	return "8080"
}
