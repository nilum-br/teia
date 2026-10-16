package promapi

import "testing"

func TestScopeAddsTheClusterToEverySelector(t *testing.T) {
	scope := Scope{"cluster": "east"}
	cases := []struct{ in, want string }{
		{`up`, `up{cluster="east"}`},
		{`rate(http_requests_total[5m])`, `rate(http_requests_total{cluster="east"}[5m])`},
		{`sum by (job) (rate(x{status="500"}[5m]))`, `sum by (job) (rate(x{cluster="east",status="500"}[5m]))`},
		{`sum(rate(x[5m])) by (job)`, `sum(rate(x{cluster="east"}[5m])) by (job)`},
		{`SUM BY (job) (x) AND y`, `SUM BY (job) (x{cluster="east"}) AND y{cluster="east"}`},
		{`sum without (instance) (x)`, `sum without (instance) (x{cluster="east"})`},
		{`a / on(job) group_left(team) b`, `a{cluster="east"} / on(job) group_left(team) b{cluster="east"}`},
		{`a * on(job) group_left b`, `a{cluster="east"} * on(job) group_left b{cluster="east"}`},
		{`a unless ignoring(x) b`, `a{cluster="east"} unless ignoring(x) b{cluster="east"}`},
		{`{__name__="up"}`, `{cluster="east",__name__="up"}`},
		{`x{}`, `x{cluster="east"}`},
		{`x{a="b",}`, `x{cluster="east",a="b",}`},
		{`histogram_quantile(0.95, sum by (le, src) (rate(d_bucket{reporter="source"}[5m]))) / 1000`,
			`histogram_quantile(0.95, sum by (le, src) (rate(d_bucket{cluster="east",reporter="source"}[5m]))) / 1000`},
		{`label_replace(up, "dst", "$1", "src", "(.*)")`, `label_replace(up{cluster="east"}, "dst", "$1", "src", "(.*)")`},
		{`label_replace(x, "a", "{}", "b", "(.*)")`, `label_replace(x{cluster="east"}, "a", "{}", "b", "(.*)")`},
		{`count(x) > 0`, `count(x{cluster="east"}) > 0`},
		{`x offset 5m`, `x{cluster="east"} offset 5m`},
		{`x @ start()`, `x{cluster="east"} @ start()`},
		{`max_over_time(rate(x[5m])[1h:1m])`, `max_over_time(rate(x{cluster="east"}[5m])[1h:1m])`},
		{`timestamp(x) and (y > 0)`, `timestamp(x{cluster="east"}) and (y{cluster="east"} > 0)`},
		{`x > bool 1e3`, `x{cluster="east"} > bool 1e3`},
		{`x * 0x10 + 1.5e-3 - Inf`, `x{cluster="east"} * 0x10 + 1.5e-3 - Inf`},
		{`-x`, `-x{cluster="east"}`},
		{`job:http_requests:rate5m`, `job:http_requests:rate5m{cluster="east"}`},
		{`topk(5, sum by (a) (rate(x[5m])))`, `topk(5, sum by (a) (rate(x{cluster="east"}[5m])))`},
		{`count_values("version", build_info)`, `count_values("version", build_info{cluster="east"})`},
		{`quantile(0.9, x)`, `quantile(0.9, x{cluster="east"})`},
		{"x # a comment {\n+ y", "x{cluster=\"east\"} # a comment {\n+ y{cluster=\"east\"}"},
		{`vector(1)`, `vector(1)`},
		{`time() - 1`, `time() - 1`},
		{`{"my.metric", "a.b"="c"}`, `{"my.metric",cluster="east", "a.b"="c"}`},
		{`{"my.metric"}`, `{"my.metric",cluster="east"}`},
		// An explicit matcher on the label wins, whatever its operator.
		{`x{cluster="west"}`, `x{cluster="west"}`},
		{`x{cluster=~"e.*"} + y`, `x{cluster=~"e.*"} + y{cluster="east"}`},
		{`x{"cluster"!="a"}`, `x{"cluster"!="a"}`},
	}
	for _, c := range cases {
		got, err := scope.Apply(c.in, PromQL)
		if err != nil || got != c.want {
			t.Errorf("Apply(%q)\n got %q, %v\nwant %q", c.in, got, err, c.want)
		}
	}
}

func TestScopeKeepsSeveralLabelsInOrder(t *testing.T) {
	got, err := Scope{"region": "us", "cluster": "east"}.Apply(`x{job="a"}`, PromQL)
	if want := `x{cluster="east",region="us",job="a"}`; err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
	got, err = Scope{"k8s.cluster.name": `a"b`}.Apply(`x`, PromQL)
	if want := `x{"k8s.cluster.name"="a\"b"}`; err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestScopeOfLogQLTouchesOnlyStreamSelectors(t *testing.T) {
	scope := Scope{"cluster": "east"}
	cases := []struct{ in, want string }{
		{`{app="x"} |= "error"`, `{cluster="east",app="x"} |= "error"`},
		{`sum by (app) (count_over_time({app="x"} | json | line_format "{{.msg}}" [5m]))`,
			`sum by (app) (count_over_time({cluster="east",app="x"} | json | line_format "{{.msg}}" [5m]))`},
		{`sum by (src, dst) (rate({job="access"} | logfmt | status >= 500 [5m]))`,
			`sum by (src, dst) (rate({cluster="east",job="access"} | logfmt | status >= 500 [5m]))`},
		{`{app="x", cluster="b"}`, `{app="x", cluster="b"}`},
	}
	for _, c := range cases {
		got, err := scope.Apply(c.in, LogQL)
		if err != nil || got != c.want {
			t.Errorf("Apply(%q)\n got %q, %v\nwant %q", c.in, got, err, c.want)
		}
	}
}

// A query the scope cannot read is never sent without the scope.
func TestScopeRejectsWhatItCannotRead(t *testing.T) {
	for _, q := range []string{`x{a="b"`, `rate(x[5m]`, `label_replace(x, "a`, `sum by (a (x)`} {
		if got, err := (Scope{"cluster": "east"}).Apply(q, PromQL); err == nil {
			t.Errorf("Apply(%q) = %q; want an error", q, got)
		}
	}
	if got, err := Scope(nil).Apply(`x{a="b"`, PromQL); err != nil || got != `x{a="b"` {
		t.Errorf("an empty scope must leave the query alone: %q, %v", got, err)
	}
}

func TestScopeValidate(t *testing.T) {
	if err := (Scope{"cluster": ""}).Validate(); err == nil {
		t.Error("an empty value must be rejected")
	}
	if err := (Scope{`a"b`: "x"}).Validate(); err == nil {
		t.Error("a label name with a quote must be rejected")
	}
	if err := (Scope{"k8s.cluster.name": "x"}).Validate(); err != nil {
		t.Errorf("a dotted label name is valid in Prometheus 3: %v", err)
	}
}
