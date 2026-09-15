package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
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

	res, err := computeResult(*req.Bands)
	if err != nil {
		// 输入已限定在 40−140 / 0−40，C 落在 0−140，正常不会走到这里；
		// 一旦发生仍按整单失败处理，绝不返回部分结果。
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "合成声级计算失败：" + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// handleCompare 是耳罩对比端点：POST /api/compare。
// 复用同一组六个现场声级，分别套用甲、乙两份逐带衰减，沿用具整单校验与同一合成算法，
// 产出两份完整结果，并依据未舍入合成声级返回较优耳罩（或效果相同）。
// 任一候选缺行、越界或格式错误，整单返回 422，绝不返回另一候选的结果。
func handleCompare(w http.ResponseWriter, r *http.Request) {
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

	_, parsed, errs := validateCompare(body)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
			Error:  "对比整单校验未通过，请修正标红字段后重新提交",
			Fields: errs,
		})
		return
	}

	// 两个候选都通过校验后才开始计算，保证不会产出任何单边结果。
	resA, err := computeResult(parsed[candidateA])
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "合成声级计算失败：" + err.Error()})
		return
	}
	resB, err := computeResult(parsed[candidateB])
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "合成声级计算失败：" + err.Error()})
		return
	}

	ca := compareCandidateResult{Label: candidateA,
		Rows: resA.Rows, Formula: resA.Formula, Substitution: resA.Substitution,
		ExactLevel: resA.ExactLevel, DisplayLevel: resA.DisplayLevel}
	cb := compareCandidateResult{Label: candidateB,
		Rows: resB.Rows, Formula: resB.Formula, Substitution: resB.Substitution,
		ExactLevel: resB.ExactLevel, DisplayLevel: resB.DisplayLevel}

	winner, tie := decideWinner(ca, cb)
	conclusion := compareConclusionText(ca, cb, winner, tie)

	writeJSON(w, http.StatusOK, compareResponse{
		Candidates: []compareCandidateResult{ca, cb},
		Winner:     winner,
		Tie:        tie,
		Conclusion: conclusion,
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
	mux.HandleFunc("/api/compare", withLogging(handleCompare))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "未找到：可用端点 GET /health、POST /api/calculate、POST /api/compare"})
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
