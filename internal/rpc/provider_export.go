 //go:build !light_client
 // +build !light_client


package rpc

// /eternity/* 핸들러가 사용할 프로바이더 (엔진 등)
var eternityProvider EternityProvider

// 엔진 쪽에서 세팅
func SetEternityProvider(p EternityProvider) { eternityProvider = p }

// main.go에서 꺼내 쓸 때 사용
func GetEternityProvider() EternityProvider { return eternityProvider }
