package engine

import (
	"log"
	"time"
)

// Picker: 슬롯별 제안자를 고르는 최소 인터페이스
type Picker interface {
	PickProposer(slot uint64) string
}

type Engine struct {
	Logger  *log.Logger
	Picker  Picker
	Metrics MetricsSink
	slot    uint64
}

// 내부 1라운드 실행 (스텁)
func (e *Engine) runSlotRound() {
	// proposal
	time.Sleep(100 * time.Millisecond)
	if e.Metrics != nil {
		e.Metrics.IncCounter("proposal_total")
	}
	if e.Logger != nil {
		e.Logger.Printf("[consensus] proposal done (100ms)")
	}

	// prevote
	time.Sleep(200 * time.Millisecond)
	if e.Metrics != nil {
		e.Metrics.IncCounter("prevote_total")
	}
	if e.Logger != nil {
		e.Logger.Printf("[consensus] prevote done (200ms)")
	}

	// precommit
	time.Sleep(200 * time.Millisecond)
	if e.Metrics != nil {
		e.Metrics.IncCounter("precommit_total")
	}
	if e.Logger != nil {
		e.Logger.Printf("[consensus] precommit done (200ms)")
	}

	// commit
	time.Sleep(100 * time.Millisecond)
	if e.Metrics != nil {
		e.Metrics.IncCounter("commit_total")
	}
	if e.Logger != nil {
		e.Logger.Printf("[consensus] commit done (100ms)")
	}
}

// 무한 루프 버전
func (e *Engine) StartConsensusLoop() {
	if e.Logger == nil {
		e.Logger = log.Default()
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		<-t.C
		e.runSlotRound()
		e.slot++
	}
}

// 지정된 라운드 수만 실행하고 종료 (중복 정의 금지!)
func (e *Engine) StartConsensusLoopWithLimit(limit int) {
	if e.Logger == nil {
		e.Logger = log.Default()
	}
	if limit <= 0 {
		e.StartConsensusLoop()
		return
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for i := 0; i < limit; i++ {
		<-t.C
		e.runSlotRound()
		e.slot++
	}
}
