package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nilum-br/teia/internal/config"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/review"
	"github.com/nilum-br/teia/internal/source/declared"
)

type repeatedFlag []string

func (v *repeatedFlag) String() string     { return strings.Join(*v, ", ") }
func (v *repeatedFlag) Set(s string) error { *v = append(*v, s); return nil }

func diff(args []string) error { return diffTo(args, os.Stdout) }

func diffTo(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	beforeFile := fs.String("before", "", "older saved snapshot JSON")
	afterFile := fs.String("after", "", "newer saved snapshot JSON")
	snapshotFile := fs.String("snapshot", "", "saved snapshot JSON for contract review")
	configFile := fs.String("config", "", "optional Teia config; apply the same aliases as the saved snapshot")
	format := fs.String("format", "text", "text, json, or additive flows YAML")
	var flowFiles, accept repeatedFlag
	fs.Var(&flowFiles, "flows", "flows file to compare (repeatable); omitted means an empty contract")
	fs.Var(&accept, "accept", "explicit accepted addition caller->callee=owning-team rationale (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("teia diff accepts named flags only; see teia help")
	}
	if *format != "text" && *format != "json" && *format != "flows" {
		return errors.New("--format must be text, json or flows")
	}
	var report review.Report
	var snapshot model.Snapshot
	if *snapshotFile != "" {
		if *beforeFile != "" || *afterFile != "" {
			return errors.New("use --snapshot/--flows or --before/--after, not both")
		}
		var err error
		snapshot, err = readSnapshot(*snapshotFile)
		if err != nil {
			return err
		}
		var docs []declared.Document
		for _, file := range flowFiles {
			data, err := readReviewFile(file)
			if err != nil {
				return err
			}
			parsed, err := declared.Parse(data)
			if err != nil {
				return fmt.Errorf("reading flows %s: %w", file, err)
			}
			if len(parsed) == 0 {
				return fmt.Errorf("%s contains no Flows documents", file)
			}
			for _, doc := range parsed {
				if errs := declared.Validate(doc); len(errs) > 0 {
					return fmt.Errorf("invalid flows %s: %w", file, errors.Join(errs...))
				}
			}
			docs = append(docs, parsed...)
		}
		var aliases map[string]string
		if *configFile != "" {
			cfg, err := config.Load(*configFile)
			if err != nil {
				return err
			}
			aliases = cfg.Aliases
		}
		report = review.Contract(docs, snapshot, aliases)
	} else {
		if *beforeFile == "" || *afterFile == "" {
			return errors.New("use teia diff --before OLD.json --after NEW.json, or --snapshot GRAPH.json [--flows FILE]")
		}
		if len(flowFiles) > 0 || len(accept) > 0 || *format == "flows" || *configFile != "" {
			return errors.New("flows and acceptance options require --snapshot")
		}
		before, err := readSnapshot(*beforeFile)
		if err != nil {
			return err
		}
		after, err := readSnapshot(*afterFile)
		if err != nil {
			return err
		}
		report = review.Snapshots(before, after)
	}
	if *format == "flows" {
		accepted := map[string]string{}
		for _, item := range accept {
			key, reason, ok := strings.Cut(item, "=")
			if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(reason) == "" {
				return errors.New("--accept must be 'caller->callee=nonempty owning-team rationale'")
			}
			if _, exists := accepted[key]; exists {
				return fmt.Errorf("duplicate --accept for %q", key)
			}
			accepted[key] = reason
		}
		return review.Flows(w, report, snapshot, accepted)
	}
	if len(accept) > 0 {
		return errors.New("--accept requires --format flows; text/json are read-only comparisons")
	}
	if *format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return writeReviewText(w, report)
}

const maxReviewFile = 64 << 20

func readReviewFile(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReviewFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReviewFile {
		return nil, fmt.Errorf("review input %s exceeds 64 MiB", file)
	}
	return data, nil
}

func readSnapshot(file string) (model.Snapshot, error) {
	var snapshot model.Snapshot
	data, err := readReviewFile(file)
	if err != nil {
		return snapshot, err
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(data, &shape); err != nil {
		return snapshot, fmt.Errorf("reading snapshot %s: %w", file, err)
	}
	if _, nodes := shape["nodes"]; !nodes {
		return snapshot, fmt.Errorf("%s is not a snapshot: missing nodes", file)
	}
	if _, edges := shape["edges"]; !edges {
		return snapshot, fmt.Errorf("%s is not a snapshot: missing edges", file)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("reading snapshot %s: %w", file, err)
	}
	if err := review.ValidateSnapshot(snapshot); err != nil {
		return snapshot, fmt.Errorf("%s: %w", file, err)
	}
	return snapshot, nil
}

func writeReviewText(w io.Writer, report review.Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Teia %s review\n", report.Mode)
	for _, change := range report.Changes {
		fmt.Fprintf(&b, "%s %s", change.Type, change.Subject)
		if len(change.Fields) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(change.Fields, ", "))
		}
		b.WriteByte('\n')
	}
	for _, candidate := range report.Candidates {
		fmt.Fprintf(&b, "%s %s protocol=%s port=%d identity=%s\n  %s\n", candidate.Action, candidate.ID, candidate.Edge.Protocol, candidate.Edge.Port, candidate.Identity, candidate.Detail)
	}
	for _, flow := range report.Unobserved {
		fmt.Fprintf(&b, "unobserved %s", flow.ID)
		if flow.ExpectedFrequency != "" {
			fmt.Fprintf(&b, " expectedFrequency=%s", flow.ExpectedFrequency)
		}
		fmt.Fprintf(&b, "\n  %s\n", flow.Detail)
	}
	if len(report.Changes)+len(report.Candidates)+len(report.Unobserved) == 0 {
		b.WriteString("No topology, contract or health-state differences.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
