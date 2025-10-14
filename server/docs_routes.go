package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed static/openapi.json
var openapiJSON []byte

func RegisterDocsRoutes(mux *http.ServeMux) {
	// /openapi.json
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		allowCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		// 머지 결과 우선, 실패하면 정적 파일로 폴백
		if data, err := buildMergedOpenAPI(); err == nil {
			_, _ = w.Write(data)
			return
		}
		_, _ = w.Write(openapiJSON)
	})

	// /openapi/<name>.json
	mux.HandleFunc("/openapi/", func(w http.ResponseWriter, r *http.Request) {
		allowCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/openapi/")
		if name == "" || strings.Contains(name, "..") {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid name")
			return
		}

		// special: merged.json ? 議곌컖?ㅼ쓣 ?⑹퀜??利됱떆 ?앹꽦
		if name == "merged.json" {
			data, err := buildMergedOpenAPI()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "merge_failed", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}

		// 洹??? known dirs ?먯꽌 ?뚯씪 ?쒕튃
		if b, ok := readOpenAPIPart(name); ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(b)
			return
		}
		writeJSONError(w, http.StatusNotFound, "bad_request", "404 page not found")
	})

	// /docs and /docs/
	mux.HandleFunc("/docs", docsHTMLHandler)
	mux.HandleFunc("/docs/", docsHTMLHandler)
}

func allowCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"applied": false,
		"ok":      false,
		"error": map[string]interface{}{
			"code":    code,
			"message": msg,
		},
	})
}

func docsHTMLHandler(w http.ResponseWriter, r *http.Request) {
	allowCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, docsHTML)
}

const docsHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<title>THO Node API Docs</title>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>html,body{margin:0;padding:0}.topbar{display:none}</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.onload = () => {
  const ui = SwaggerUIBundle({
    url: '/openapi.json',
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis],
    layout: 'BaseLayout',
  });
  window.ui = ui;
};
</script>
</body>
</html>`

// ---------- OpenAPI merge (tx/rounds/policy/health) ----------

var openAPIDirs = []string{
	"server/static/openapi",
	"server/docs/openapi",
}

func readOpenAPIPart(name string) ([]byte, bool) {
	// ?ㅽ뻾 以?working dir 湲곗? + ?곷?寃쎈줈
	for _, d := range openAPIDirs {
		fp := filepath.Join(d, name)
		if b, err := os.ReadFile(fp); err == nil {
			// BOM ?쒓굅 諛⑹?: bytes 洹몃?濡??꾨떖
			return b, true
		}
	}
	return nil, false
}

func buildMergedOpenAPI() ([]byte, error) {
	// 猷⑦듃 怨④꺽
	doc := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":   "THO Node API",
			"version": "0.1.0",
		},
		"servers": []interface{}{
			map[string]interface{}{"url": "/"},
		},
		"paths": map[string]interface{}{},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{},
		},
	}

	parts := []string{"tx.json", "rounds.json", "policy.json", "health.json", "wallet.json"}

	for _, p := range parts {
		raw, ok := readOpenAPIPart(p)
		if !ok {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(stripBOM(raw), &m); err != nil {
			// 議곌컖??源⑥졇 ?덉뼱???ㅻⅨ 議곌컖 吏꾪뻾
			continue
		}
		mergePathsAndSchemas(doc, m)
	}

	// ?덉쟾?μ튂: /health ?꾨씫 ???먮룞 ?쎌엯(+ ?ㅽ궎留?蹂닿컯)
	ensureHealth(doc)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return out, nil
}

func mergePathsAndSchemas(dst, src map[string]interface{}) {
	// paths
	if sp, ok := src["paths"].(map[string]interface{}); ok {
		dp := getMap(dst, "paths")
		for k, v := range sp {
			// 異⑸룎 ??src ?곗꽑 ??뼱?곌린
			dp[k] = v
		}
		dst["paths"] = dp
	}
	// components.schemas
	if sc, ok := src["components"].(map[string]interface{}); ok {
		if ss, ok2 := sc["schemas"].(map[string]interface{}); ok2 {
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

func ensureHealth(doc map[string]interface{}) {
	paths := getMap(doc, "paths")
	if _, ok := paths["/health"]; ok {
		return
	}
	// ?ㅽ궎留?蹂닿컯
	components := getMap(doc, "components")
	schemas := getMap(components, "schemas")
	if _, ok := schemas["HealthResponse"]; !ok {
		schemas["HealthResponse"] = map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"status"},
			"properties": map[string]interface{}{
				"status": map[string]interface{}{
					"type":    "string",
					"enum":    []interface{}{"ok", "degraded"},
					"example": "ok",
				},
			},
		}
	}
	components["schemas"] = schemas
	doc["components"] = components

	paths["/health"] = map[string]interface{}{
		"get": map[string]interface{}{
			"summary": "Health",
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "OK",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"$ref": "#/components/schemas/HealthResponse",
							},
							"examples": map[string]interface{}{
								"ok": map[string]interface{}{
									"value": map[string]interface{}{"status": "ok"},
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

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	n := map[string]interface{}{}
	m[key] = n
	return n
}

func stripBOM(b []byte) []byte {
	// UTF-8 BOM ?쒓굅
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
