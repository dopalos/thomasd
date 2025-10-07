//go:build light_client

package rpc

import (
	"net/http"

	"thomasd/internal/app"
)

func installFullnodeOnlyRoutes(_ *http.ServeMux, _ *app.Engine) {}
