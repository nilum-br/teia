package main

import (
	"net/http"
	"reflect"
	"testing"
)

func TestOperationRoutesKeepSharedFrontendCallsSeparate(t *testing.T) {
	calls := parseCalls("orders=http://orders/,catalog=http://catalog/,recommendations=http://recommendations/")
	for _, tc := range []struct {
		path string
		want []string
	}{{"/checkout", []string{"orders"}}, {"/browse", []string{"catalog", "recommendations"}}} {
		var names []string
		for _, call := range operationCalls(calls, "web", tc.path) {
			names = append(names, call.name)
		}
		if !reflect.DeepEqual(names, tc.want) {
			t.Fatalf("%s calls %v", tc.path, names)
		}
	}
	if len(operationCalls(calls, "web", "/")) != 3 || len(operationCalls(calls, "orders", "/checkout")) != 3 {
		t.Fatal("changed the default lab flow")
	}
	if operationRoute("/users/secret") != "/{path...}" {
		t.Fatal("raw path used as operation")
	}
	method, target := operationTarget("http://web:8080/", 0, true)
	if method != http.MethodPost || target != "http://web:8080/checkout" {
		t.Fatal("checkout generator")
	}
	method, target = operationTarget("http://web:8080/", 1, true)
	if method != http.MethodGet || target != "http://web:8080/browse" {
		t.Fatal("browse generator")
	}
	_, target = operationTarget("http://web:8080/custom", 1, false)
	if target != "http://web:8080/custom" {
		t.Fatal("opt-in changed an existing generator")
	}
}

// Only web's entry spans name a journey; the calls below inherit it in teia.
func TestOperationJourneyTagsOnlyTheEntry(t *testing.T) {
	for _, tc := range []struct{ service, path, want string }{
		{"web", "/checkout", "checkout"}, {"web", "/browse", "browse"}, {"web", "/", ""}, {"orders", "/checkout", ""},
	} {
		if got := operationJourney(tc.service, tc.path); got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.service, tc.path, got, tc.want)
		}
	}
}
