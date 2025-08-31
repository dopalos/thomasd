package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"thomasd/internal/app"
	"thomasd/internal/rpc"
)

const buildTag = "debug-docs"

// --- OpenAPI 사양(JSON) & Swagger HTML ---
var openapiJSON = []byte(`{
  "openapi": "3.0.3",
  "info": { "title": "THO Node API", "version": "0.1.0" },
  "servers": [ { "url": "/" } ],
  "paths": {
    "/health": { "get": { "summary": "Health", "responses": { "200": { "description": "OK" } } } },
    "/policy": { "get": { "summary": "Policy", "responses": { "200": { "description": "OK" } } } },
    "/round/latest/signed": { "get": { "summary": "Latest signed header", "responses": { "200": { "description": "OK" } } } },
    "/round/{round}/signed": { "get": { "summary": "Round signed header", "parameters": [
      { "name": "round", "in": "path", "required": true, "schema": { "type": "integer", "format": "int64" } }
    ], "responses": { "200": { "description": "OK" } } } },
    "/tx": { "post": { "summary": "Submit tx", "responses": { "200": { "description": "Queued" } } } }
  },
  "components": { "schemas": {} }
}`)

const docsHTML = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>THO Node API Docs</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>html,body{margin:0;padding:0}.topbar{display:none}</style>
</head><body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.onload=()=>{ const ui=SwaggerUIBundle({
  url:'/openapi/merged.json',
  dom_id:'#swagger-ui',
  presets:[SwaggerUIBundle.presets.apis],
  layout:'BaseLayout'
}); window.ui=ui; };
</script>
</body></html>`

func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(docsHTML))
}

func openapiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(openapiJSON)
}

// /openapi/{name}.json 조각 서빙
func openapiFragmentHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/openapi/"))
	if name == "" || strings.Contains(name, "/") || !strings.HasSuffix(strings.ToLower(name), ".json") {
		http.NotFound(w, r)
		return
	}

	// 우선순위: <repoRoot>\server\static\openapi\<name> → <repoRoot>\server\docs\openapi\<name>
	// 폴백: 현재 작업 디렉터리 기준 동일 상대경로
	var candidates []string
	if exePath, err := os.Executable(); err == nil && exePath != "" {
		exeDir := filepath.Dir(exePath)
		repoRoot := filepath.Dir(exeDir) // ...\thomasd\bin → ...\thomasd
		candidates = append(candidates,
			filepath.Join(repoRoot, "server", "static", "openapi", name),
			filepath.Join(repoRoot, "server", "docs", "openapi", name),
		)
	}
	candidates = append(candidates,
		filepath.Join("server", "static", "openapi", name),
		filepath.Join("server", "docs", "openapi", name),
	)

	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(b)
			return
		}
	}
	http.NotFound(w, r)
}

/*** ---------- TEMP COMPAT SHIM for signed endpoints ---------- ***/

// 간단 레코더
type respRec struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newRec() *respRec                          { return &respRec{header: make(http.Header), status: 200} }
func (rr *respRec) Header() http.Header         { return rr.header }
func (rr *respRec) Write(b []byte) (int, error) { return rr.buf.Write(b) }
func (rr *respRec) WriteHeader(code int)        { rr.status = code }

// 헤더 복사
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

var roundSignedRe = regexp.MustCompile(`^/round/[0-9]+/signed$`)

// /round/latest/signed 및 /round/{n}/signed 응답에 scheme 주입(없거나 빈 경우 "ed25519")
func signedCompatHandler(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 대상 경로만 후킹
		if !(r.Method == http.MethodGet &&
			(r.URL.Path == "/round/latest/signed" || roundSignedRe.MatchString(r.URL.Path))) {
			inner.ServeHTTP(w, r)
			return
		}

		// 내부 처리 결과를 캡처
		rr := newRec()
		inner.ServeHTTP(rr, r)

		// JSON 아니면 그대로 패스
		ct := rr.header.Get("Content-Type")
		if rr.status != http.StatusOK || !strings.Contains(strings.ToLower(ct), "application/json") {
			copyHeader(w.Header(), rr.header)
			w.WriteHeader(rr.status)
			io.Copy(w, &rr.buf)
			return
		}

		// JSON 파싱 후 scheme 보정
		var m map[string]any
		if err := json.Unmarshal(rr.buf.Bytes(), &m); err != nil {
			// 파싱 실패 시 원본 그대로 전달
			copyHeader(w.Header(), rr.header)
			w.WriteHeader(rr.status)
			io.Copy(w, &rr.buf)
			return
		}
		s, _ := m["scheme"].(string)
		if strings.TrimSpace(s) == "" {
			m["scheme"] = "ed25519"
			b, _ := json.Marshal(m)
			// 캐시/타입 헤더 유지
			copyHeader(w.Header(), rr.header)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
			return
		}

		// 이미 scheme 있으면 그대로
		copyHeader(w.Header(), rr.header)
		w.WriteHeader(rr.status)
		io.Copy(w, &rr.buf)
	}
}

/*** ---------- merged.json 동적 생성 (조각 병합 + /health 자동보강) ---------- ***/

// 후보 경로에서 조각 읽기
func readOpenAPIPart(name string) ([]byte, bool) {
	var candidates []string
	if exePath, err := os.Executable(); err == nil && exePath != "" {
		exeDir := filepath.Dir(exePath)
		repoRoot := filepath.Dir(exeDir)
		candidates = append(candidates,
			filepath.Join(repoRoot, "server", "static", "openapi", name),
			filepath.Join(repoRoot, "server", "docs", "openapi", name),
		)
	}
	candidates = append(candidates,
		filepath.Join("server", "static", "openapi", name),
		filepath.Join("server", "docs", "openapi", name),
	)
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return b, true
		}
	}
	return nil, false
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	n := map[string]any{}
	m[key] = n
	return n
}

func mergePathsAndSchemas(dst, src map[string]any) {
	if sp, ok := src["paths"].(map[string]any); ok {
		dp := getMap(dst, "paths")
		for k, v := range sp {
			dp[k] = v // 충돌 시 src 우선
		}
		dst["paths"] = dp
	}
	if sc, ok := src["components"].(map[string]any); ok {
		if ss, ok2 := sc["schemas"].(map[string]any); ok2 {
			dc := getMap(dst, "components")
			ds := getMap(dc, "schemas")
			for k, v := range ss {
				ds[k] = v
			}
			dc["schemas"] = ds
			dst["components"] = dc
		}
	}
}

func ensureHealth(doc map[string]any) {
	paths := getMap(doc, "paths")
	if _, ok := paths["/health"]; ok {
		return
	}
	components := getMap(doc, "components")
	schemas := getMap(components, "schemas")
	if _, ok := schemas["HealthResponse"]; !ok {
		schemas["HealthResponse"] = map[string]any{
			"type":     "object",
			"required": []any{"status"},
			"properties": map[string]any{
				"status": map[string]any{
					"type":    "string",
					"enum":    []any{"ok", "degraded"},
					"example": "ok",
				},
			},
		}
	}
	components["schemas"] = schemas
	doc["components"] = components

	paths["/health"] = map[string]any{
		"get": map[string]any{
			"summary": "Health",
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"$ref": "#/components/schemas/HealthResponse",
							},
							"examples": map[string]any{
								"ok": map[string]any{
									"value": map[string]any{"status": "ok"},
								},
							},
						},
					},
				},
			},
		},
	}
	doc["paths"] = paths
}

func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func buildMergedOpenAPI() ([]byte, error) {
	doc := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "THO Node API",
			"version": "0.1.0",
		},
		"servers": []any{
			map[string]any{"url": "/"},
		},
		"paths": map[string]any{},
		"components": map[string]any{
			"schemas": map[string]any{},
		},
	}
	parts := []string{"tx.json", "rounds.json", "policy.json", "health.json"}

	for _, p := range parts {
		raw, ok := readOpenAPIPart(p)
		if !ok {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(stripBOM(raw), &m); err != nil {
			continue // 조각이 깨져 있어도 다른 조각 진행
		}
		mergePathsAndSchemas(doc, m)
	}
	// 안전장치
	ensureHealth(doc)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return out, nil
}

func openapiMergedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	data, err := buildMergedOpenAPI()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"applied": false,
			"error": map[string]any{
				"code":    "merge_failed",
				"message": err.Error(),
			},
		})
		return
	}
	_, _ = w.Write(data)
}

/*** ---------- /metrics (Prometheus format, 가드: THOMAS_ENABLE_METRICS=1) ---------- ***/

// 내부 핸들러를 직접 호출해서 JSON 받아오기
func callInnerJSON(inner http.Handler, method, path string) (map[string]any, int, http.Header) {
	req, _ := http.NewRequest(method, path, nil)
	rr := newRec()
	inner.ServeHTTP(rr, req)
	var m map[string]any
	if rr.status == http.StatusOK && strings.Contains(strings.ToLower(rr.header.Get("Content-Type")), "application/json") {
		_ = json.Unmarshal(rr.buf.Bytes(), &m)
	}
	return m, rr.status, rr.header
}

func metricsHandler(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 베이식 헤더
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		var b bytes.Buffer

		// build info
		b.WriteString("# HELP thomas_build_info Build metadata\n")
		b.WriteString("# TYPE thomas_build_info gauge\n")
		b.WriteString("thomas_build_info{tag=\"" + buildTag + "\",version=\"0.1.0\"} 1\n\n")

		// /health
		health, code, _ := callInnerJSON(inner, http.MethodGet, "/health")
		ok := 0.0
		if code == 200 && strings.EqualFold(strings.TrimSpace(stringify(health["status"])), "ok") {
			ok = 1.0
		}
		b.WriteString("# HELP thomas_health_ok 1 if /health returns status=ok\n")
		b.WriteString("# TYPE thomas_health_ok gauge\n")
		b.WriteString("thomas_health_ok " + f64(ok) + "\n\n")

		// /round/latest/signed
		signed, code2, _ := callInnerJSON(inner, http.MethodGet, "/round/latest/signed")
		if code2 == 200 {
			header, _ := signed["header"].(map[string]any)
			round := i64(header["round"])
			fromH := i64(header["from_height"])
			toH := i64(header["to_height"])
			txCnt := i64(header["tx_count"])
			ts := i64(header["time_utc"])

			b.WriteString("# HELP thomas_round_latest Latest round number\n")
			b.WriteString("# TYPE thomas_round_latest gauge\n")
			b.WriteString("thomas_round_latest " + f64(float64(round)) + "\n")

			b.WriteString("# HELP thomas_height_to_latest Latest block height (to_height)\n")
			b.WriteString("# TYPE thomas_height_to_latest gauge\n")
			b.WriteString("thomas_height_to_latest " + f64(float64(toH)) + "\n")

			b.WriteString("# HELP thomas_tx_count_latest Tx count of latest round\n")
			b.WriteString("# TYPE thomas_tx_count_latest gauge\n")
			b.WriteString("thomas_tx_count_latest " + f64(float64(txCnt)) + "\n")

			b.WriteString("# HELP thomas_round_time_utc Latest round time (unix seconds)\n")
			b.WriteString("# TYPE thomas_round_time_utc gauge\n")
			b.WriteString("thomas_round_time_utc " + f64(float64(ts)) + "\n\n")

			_ = fromH // 남겨둠(필요하면 지표 추가)
		}

		// Go 런타임
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		b.WriteString("# HELP go_mem_alloc_bytes Process memory allocation\n")
		b.WriteString("# TYPE go_mem_alloc_bytes gauge\n")
		b.WriteString("go_mem_alloc_bytes " + f64(float64(ms.Alloc)) + "\n")

		b.WriteString("# HELP go_goroutines Number of goroutines\n")
		b.WriteString("# TYPE go_goroutines gauge\n")
		b.WriteString("go_goroutines " + f64(float64(runtime.NumGoroutine())) + "\n")

		_, _ = w.Write(b.Bytes())
	}
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func i64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	default:
		return 0
	}
}

func f64(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

/*** ----------------------------------------------------------- ***/

func main() {
	log.SetOutput(os.Stdout)

	eng := app.NewEngine()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("[STARTUP] listen error: %v", err)
		for {
			time.Sleep(10 * time.Second)
		}
	}
	addr := ln.Addr().String()

	inner := rpc.WithJSONErrors(rpc.NewRouter(eng))

	mux := http.NewServeMux()

	// ✅ 서명 엔드포인트 호환 레이어: /round/latest/signed, /round/{n}/signed 우선 매칭
	mux.HandleFunc("/round/", signedCompatHandler(inner))
	mux.HandleFunc("/round/latest/signed", signedCompatHandler(inner)) // 중복 보호

	// 기본 라우터
	mux.Handle("/", inner)

	// 문서 노출 가드
	if os.Getenv("THOMAS_ENABLE_DOCS") == "1" {
		// 스펙 최소본
		mux.HandleFunc("/openapi.json", openapiHandler)
		// 동적 merged (가장 구체적인 경로 먼저 등록)
		mux.HandleFunc("/openapi/merged.json", openapiMergedHandler)
		// 조각 서빙
		mux.HandleFunc("/openapi/", openapiFragmentHandler)
		// Swagger UI
		mux.HandleFunc("/docs", docsHandler)
		mux.HandleFunc("/docs/", docsHandler)
	}

	// /metrics 가드
	if os.Getenv("THOMAS_ENABLE_METRICS") == "1" {
		mux.HandleFunc("/metrics", metricsHandler(inner))
	}

	srv := &http.Server{Handler: mux}

	log.Printf("[OK] thomasd %s listening on http://%s", buildTag, addr)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[RUNTIME] serve error: %v", err)
	}
	for {
		time.Sleep(10 * time.Second)
	}
}
