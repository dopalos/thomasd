package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Proof 데이터 모델 (예시)
type Proof struct {
	Root     []byte   `json:"root"`
	Siblings [][]byte `json:"siblings,omitempty"`
}

// ProofGenerator 인터페이스
type ProofGenerator interface {
	Generate(height uint64) (Proof, error)
}

// DummyProofGen: 데모용
type DummyProofGen struct{}

func NewDummyProofGen() *DummyProofGen { return &DummyProofGen{} }

func (d *DummyProofGen) Generate(height uint64) (Proof, error) {
	// "height=NN"를 root에 심는 매우 단순한 예시
	root := make([]byte, 32)
	copy(root, []byte("height="+strconv.FormatUint(height, 10)))
	return Proof{Root: root}, nil
}

// (실구현용 자리) RealProofGen — 중복 정의 주의!
type RealProofGen struct {
	// TODO: DB 핸들 등 의존성
}

func NewRealProofGen( /* deps */ ) *RealProofGen { return &RealProofGen{} }

func (r *RealProofGen) Generate(height uint64) (Proof, error) {
	// TODO: height에서 상태 루트/머클 경로 생성
	return Proof{}, nil
}

// HTTP 서버 생성
func NewHTTPServer(addr string, gen ProofGenerator) *http.Server {
	mux := http.NewServeMux()

	// GET /block/{height}/light-proof
	mux.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/block/")
		if !strings.HasSuffix(path, "/light-proof") {
			http.NotFound(w, r)
			return
		}
		part := strings.TrimSuffix(path, "/light-proof")
		h, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}

		p, err := gen.Generate(h)
		if err != nil {
			http.Error(w, "failed to generate proof", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"height": h,
			"proof":  p,
		})
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return srv
}

// (선택) 간단한 서버 부트 헬퍼
func Serve(addr string, gen ProofGenerator) error {
	s := NewHTTPServer(addr, gen)
	log.Println("[api] listen", addr)
	return s.ListenAndServe()
}
