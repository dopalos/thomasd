package engine

// MetricsSink: 합의 루프가 호출하는 최소 메트릭 인터페이스
type MetricsSink interface {
	ObservePhase(name string, ms int64)
	IncCounter(name string)
}

// MemMetrics: 아주 단순한 in-memory 카운터 구현
type MemMetrics struct{ C map[string]int64 }

func (m *MemMetrics) ObservePhase(name string, ms int64) {}

func (m *MemMetrics) IncCounter(name string) {
	if m.C == nil {
		m.C = make(map[string]int64)
	}
	m.C[name]++
}
