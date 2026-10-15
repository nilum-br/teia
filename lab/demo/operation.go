package main

import (
	"net/http"
	"net/url"
)

func operationRoute(path string) string {
	switch path {
	case "/", "/checkout", "/browse":
		return path
	default:
		return "/{path...}"
	}
}

func operationCalls(calls []downstream, service, path string) []downstream {
	if service != "web" || path != "/checkout" && path != "/browse" {
		return calls
	}
	var selected []downstream
	for _, call := range calls {
		if path == "/checkout" && call.name == "orders" || path == "/browse" && (call.name == "catalog" || call.name == "recommendations") {
			selected = append(selected, call)
		}
	}
	return selected
}

func operationTarget(target string, iteration int, enabled bool) (string, string) {
	if !enabled {
		return http.MethodGet, target
	}
	u, err := url.Parse(target)
	if err != nil {
		return http.MethodGet, target
	}
	method := http.MethodPost
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/checkout", "", "", ""
	if iteration%2 != 0 {
		method, u.Path = http.MethodGet, "/browse"
	}
	return method, u.String()
}
