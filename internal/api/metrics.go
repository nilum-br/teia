package api

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/nilum-br/teia/internal/model"
)

// metricsHandler exposes the snapshot in the Prometheus text format, so any
// alerting stack can react to drift or to coverage dropping, e.g.
//
//	count(teia_edge_info{state="ghost"}) > 0
//	teia_coverage_ratio{scope="visibility"} < 0.8
//
// metricsHandler writes every cluster's series, or with ?cluster=name only
// that cluster's, so each cluster can be its own scrape target.
func metricsHandler(snaps func() []model.Snapshot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all := snaps()
		if name := r.URL.Query().Get("cluster"); name != "" {
			i := slices.IndexFunc(all, func(s model.Snapshot) bool { return s.Cluster == name })
			if i < 0 {
				writeError(w, http.StatusNotFound, fmt.Sprintf("no cluster %q", name))
				return
			}
			all = all[i : i+1]
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writeMetrics(w, all...)
	}
}

// writeMetrics writes one series per value of every snapshot, each family
// once. With clusters, every series carries cluster="name".
func writeMetrics(w io.Writer, snaps ...model.Snapshot) {
	gauge := func(name, help string) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	}
	// labels prefixes a snapshot's cluster to a series' own labels.
	labels := func(s model.Snapshot, own string) string {
		switch {
		case s.Cluster == "" && own == "":
			return ""
		case s.Cluster == "":
			return "{" + own + "}"
		case own == "":
			return "{cluster=" + q(s.Cluster) + "}"
		}
		return "{cluster=" + q(s.Cluster) + "," + own + "}"
	}

	gauge("teia_coverage_ratio", "Coverage of the last discovery round (0..1).")
	for _, s := range snaps {
		fmt.Fprintf(w, "teia_coverage_ratio%s %g\n", labels(s, `scope="visibility"`), s.Coverage.Visibility)
		fmt.Fprintf(w, "teia_coverage_ratio%s %g\n", labels(s, `scope="declared"`), s.Coverage.Declared)
		fmt.Fprintf(w, "teia_coverage_ratio%s %g\n", labels(s, `scope="observed"`), s.Coverage.Observed)
	}

	gauge("teia_node_info", "One series per node in the graph.")
	for _, s := range snaps {
		for _, n := range s.Nodes {
			fmt.Fprintf(w, "teia_node_info%s 1\n", labels(s, fmt.Sprintf("id=%s,kind=%s,visibility=%s", q(n.ID), q(string(n.Kind)), q(string(n.Visibility)))))
		}
	}

	gauge("teia_edge_info", "One series per communication; state is ok, ghost (undeclared) or stale (not observed).")
	for _, s := range snaps {
		for _, e := range s.Edges {
			fmt.Fprintf(w, "teia_edge_info%s 1\n", labels(s, fmt.Sprintf("from=%s,to=%s,state=%s,protocol=%s,sources=%s",
				q(e.From), q(e.To), q(string(e.State)), q(e.Protocol), q(strings.Join(e.Sources, ",")))))
		}
	}

	gauge("teia_findings", "Open findings by rule and severity.")
	for _, s := range snaps {
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
			fmt.Fprintf(w, "teia_findings%s %d\n", labels(s, fmt.Sprintf("rule=%s,kind=%s,severity=%s", q(k[0]), q(k[1]), q(k[2]))), counts[k])
		}
	}

	gauge("teia_source_up", "1 when the source worked in the last round (state says whether it saw everything).")
	for _, s := range snaps {
		for _, src := range s.Sources {
			up := 1
			if src.State == model.SourceError {
				up = 0
			}
			fmt.Fprintf(w, "teia_source_up%s %d\n", labels(s, fmt.Sprintf("source=%s,type=%s,state=%s", q(src.Name), q(src.Type), q(string(src.State)))), up)
		}
	}

	if slices.ContainsFunc(snaps, func(s model.Snapshot) bool { return !s.GeneratedAt.IsZero() }) {
		gauge("teia_last_round_timestamp_seconds", "When the last discovery round finished.")
		for _, s := range snaps {
			if !s.GeneratedAt.IsZero() {
				fmt.Fprintf(w, "teia_last_round_timestamp_seconds%s %d\n", labels(s, ""), s.GeneratedAt.Unix())
			}
		}
	}
}

// q quotes a label value per the Prometheus text format.
func q(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}
