// Package oapi holds the oapi-codegen output: types, std-http-server
// interface, and strict-server wrapper generated from api/openapi.yaml.
//
// To regenerate after a spec change:
//
//	task oapi:gen        # or:
//	go generate ./internal/api/oapi/
//
// The generator runs as a Go tool (oapi-codegen is declared in go.mod's
// tool directive), so no separate install step is needed.
package oapi

//go:generate go tool oapi-codegen -config cfg.yaml ../../../api/openapi.yaml
