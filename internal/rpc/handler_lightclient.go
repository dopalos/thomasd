//go:build light_client

package rpc

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"thomasd/internal/app"
	"thomasd/internal/types"
)

var lightClientEngine *app.Engine

func setLightClientEngine(e *app.Engine) {
	lightClientEngine = e
}

func lightProofHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/light-proof") {
		http.NotFound(w, r)
		return
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/block/"), "/light-proof")
	h, err := strconv.ParseUint(strings.Trim(trimmed, "/"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_height"})
		return
	}

	engine := lightClientEngine
	if engine == nil && !headerLookupOverridden {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "engine_unavailable"})
		return
	}

	header, ok := headerLookup(engine, h)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "header_not_found"})
		return
	}

	headerRootHex := hex.EncodeToString(header.CommitRoot[:])
	if isZeroCommitRoot(header.CommitRoot) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                 true,
			"height":             h,
			"header_commit_root": headerRootHex,
			"bundle_found":       false,
		})
		return
	}

	if engine == nil && !bundleLookupOverridden {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "engine_unavailable"})
		return
	}

	bundle, bundleOK := bundleLookup(engine, h)
	if !bundleOK {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                 false,
			"error":              "bundle_not_found",
			"height":             h,
			"header_commit_root": headerRootHex,
		})
		return
	}

	bundleRoot, bundleRootOK := commitRootFromBundle(bundle)
	bundleRootHex := hex.EncodeToString(bundleRoot[:])
	if !bundleRootOK {
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                 false,
			"error":              "bundle_root_unavailable",
			"height":             h,
			"header_commit_root": headerRootHex,
		})
		return
	}

	if header.CommitRoot != bundleRoot {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                 false,
			"error":              "commit_root_mismatch",
			"height":             h,
			"header_commit_root": headerRootHex,
			"bundle_root":        bundleRootHex,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                 true,
		"height":             h,
		"header_commit_root": headerRootHex,
		"bundle_root":        bundleRootHex,
		"bundle_found":       true,
	})
}

func isZeroCommitRoot(root [32]byte) bool {
	return root == ([32]byte{})
}

func commitBundleAt(eng *app.Engine, height uint64) (any, bool) {
	if eng == nil {
		return nil, false
	}
	bundle, ok := eng.CommitBundleAt(height)
	if !ok {
		return nil, false
	}
	return bundle, true
}

type headerLookupFn func(any, uint64) (types.EternityHeader, bool)
type bundleLookupFn func(any, uint64) (any, bool)

var (
	headerLookup           headerLookupFn = defaultHeaderLookup
	bundleLookup           bundleLookupFn = defaultBundleLookup
	headerLookupOverridden bool
	bundleLookupOverridden bool
)

// LightProofHandler exposes the handler used by router/tests.
func LightProofHandler(w http.ResponseWriter, r *http.Request) {
	lightProofHandler(w, r)
}

// OverrideHeaderLookup lets tests supply a custom header lookup.
func OverrideHeaderLookup(fn func(any, uint64) (types.EternityHeader, bool)) {
	if fn == nil {
		headerLookup = defaultHeaderLookup
		headerLookupOverridden = false
		return
	}
	headerLookup = fn
	headerLookupOverridden = true
}

// OverrideBundleLookup lets tests supply a custom bundle lookup.
func OverrideBundleLookup(fn func(any, uint64) (any, bool)) {
	if fn == nil {
		bundleLookup = defaultBundleLookup
		bundleLookupOverridden = false
		return
	}
	bundleLookup = fn
	bundleLookupOverridden = true
}

// ResetLightClientLookups restores default lookups.
func ResetLightClientLookups() {
	headerLookup = defaultHeaderLookup
	bundleLookup = defaultBundleLookup
	headerLookupOverridden = false
	bundleLookupOverridden = false
}

func defaultHeaderLookup(eng any, height uint64) (types.EternityHeader, bool) {
	if e, ok := eng.(*app.Engine); ok && e != nil {
		return e.EternityHeaderAt(height)
	}
	return types.EternityHeader{}, false
}

func defaultBundleLookup(eng any, height uint64) (any, bool) {
	if e, ok := eng.(*app.Engine); ok && e != nil {
		return commitBundleAt(e, height)
	}
	return nil, false
}

func commitRootFromBundle(b any) ([32]byte, bool) {
	switch v := b.(type) {
	case nil:
		return [32]byte{}, false
	case types.CommitBundle:
		return v.CommitRoot, true
	case *types.CommitBundle:
		if v == nil {
			return [32]byte{}, false
		}
		return v.CommitRoot, true
	}
	if getter, ok := b.(interface{ CommitRoot() [32]byte }); ok {
		return getter.CommitRoot(), true
	}
	if rooted, ok := b.(interface{ Root() [32]byte }); ok {
		return rooted.Root(), true
	}
	return [32]byte{}, false
}
