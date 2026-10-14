package api

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/nilum-br/teia/internal/model"
)

// metricsHandler exposes the snapshot in the Prometheus text format, so any
// alerting stack can react to drift or to coverage dropping, e.g.
//
//	count(teia_edge_info{state="ghost"}) > 0
//	teia_coverage_ratio{scope="visibility"} < 0.8
func metricsHandler(snap func() model.Snapshot) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writeMetrics(w, snap())
	}
}

func writeMetrics(w io.Writer, s model.Snapshot) {
	gauge := func(name, help string) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	}

	gauge("teia_coverage_ratio", "Coverage of the last discovery round (0..1).")
	fmt.Fprintf(w, "teia_coverage_ratio{scope=\"visibility\"} %g\n", s.Coverage.Visibility)
	fmt.Fprintf(w, "teia_coverage_ratio{scope=\"declared\"} %g\n", s.Coverage.Declared)
	fmt.Fprintf(w, "teia_coverage_ratio{scope=\"observed\"} %g\n", s.Coverage.Observed)

	gauge("teia_node_info", "One series per node in the graph.")
	for _, n := range s.Nodes {
		fmt.Fprintf(w, "teia_node_info{id=%s,kind=%s,visibility=%s} 1\n", q(n.ID), q(string(n.Kind)), q(string(n.Visibility)))
	}

	gauge("teia_edge_info", "One series per communication; state is ok, ghost (undeclared) or stale (not observed).")
	for _, e := range s.Edges {
		fmt.Fprintf(w, "teia_edge_info{from=%s,to=%s,state=%s,protocol=%s,sources=%s} 1\n",
			q(e.From), q(e.To), q(string(e.State)), q(e.Protocol), q(strings.Join(e.Sources, ",")))
	}

	gauge("teia_findings", "Open findings by rule and severity.")
	counts := map[[3]string]int{}
	for _, f := range s.Findings {
		counts[[3]string{f.Rule, string(f.Kind), string(f.Severity)}]++
	}
	keys := make([][3]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.Join(keys[i][:], "|") < strings.Join(keys[j][:], "|") })
	for _, k := range keys {
		fmt.Fprintf(w, "teia_findings{rule=%s,kind=%s,severity=%s} %d\n", q(k[0]), q(k[1]), q(k[2]), counts[k])
	}

	gauge("teia_source_up", "1 when the source worked in the last round (state says whether it saw everything).")
	for _, src := range s.Sources {
		up := 1
		if src.State == model.SourceError {
			up = 0
		}
		fmt.Fprintf(w, "teia_source_up{source=%s,type=%s,state=%s} %d\n", q(src.Name), q(src.Type), q(string(src.State)), up)
	}

	if !s.GeneratedAt.IsZero() {
		gauge("teia_last_round_timestamp_seconds", "When the last discovery round finished.")
		fmt.Fprintf(w, "teia_last_round_timestamp_seconds %d\n", s.GeneratedAt.Unix())
	}
}

// q quotes a label value per the Prometheus text format.
func q(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}
