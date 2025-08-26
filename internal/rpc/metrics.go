package rpc

import "expvar"

var (
    metricBindMismatch = expvar.NewInt("rpc_bind_mismatch_total")
    metricBadSig       = expvar.NewInt("rpc_bad_signature_total")
)

func incBindMismatch() { metricBindMismatch.Add(1) }
func incBadSigJSON()   { metricBadSig.Add(1) }
