package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nilum-br/teia/internal/review"
)

func reviewCommand(args []string) error { return reviewTo(args, os.Stdout) }

func reviewTo(args []string, w io.Writer) error {
	if len(args) == 0 || args[0] != "propose" {
		return errors.New("usage: teia review propose --snapshot GRAPH.json --from CALLER --to CALLEE --rationale TEXT [--format json|markdown|flows|bundle] [--output FILE]")
	}
	fs := flag.NewFlagSet("review propose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("snapshot", "", "saved snapshot JSON; its origin is operator-provided, not attested")
	requestFile := fs.String("request", "", "exported request JSON; replaces --from, --to and --rationale")
	from := fs.String("from", "", "observed caller identity")
	to := fs.String("to", "", "observed destination identity")
	rationale := fs.String("rationale", "", "owning-team rationale for the intended communication")
	format := fs.String("format", "json", "json, markdown, flows or bundle")
	output := fs.String("output", "", "write a new file; never overwrite an existing path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || *file == "" {
		return errors.New("review propose requires --snapshot and named flags only")
	}
	if *format != "json" && *format != "markdown" && *format != "flows" && *format != "bundle" {
		return errors.New("--format must be json, markdown, flows or bundle")
	}
	snapshot, err := readSnapshot(*file)
	if err != nil {
		return err
	}
	request := review.ProposalRequest{From: *from, To: *to, Rationale: *rationale}
	if *requestFile != "" {
		conflicting := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "from" || f.Name == "to" || f.Name == "rationale" {
				conflicting = true
			}
		})
		if conflicting {
			return errors.New("--request replaces --from, --to and --rationale")
		}
		data, err := readReviewFile(*requestFile)
		if err != nil {
			return err
		}
		if len(data) > 16<<10 {
			return errors.New("proposal request exceeds 16 KiB")
		}
		var supplied struct {
			review.ProposalRequest
			GeneratedAt time.Time `json:"generatedAt"`
			HistoryID   string    `json:"historyId,omitempty"`
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&supplied); err != nil {
			return fmt.Errorf("reading proposal request: %w", err)
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			return errors.New("provide one proposal request JSON object")
		}
		if supplied.GeneratedAt.IsZero() || !supplied.GeneratedAt.Equal(snapshot.GeneratedAt) {
			return errors.New("request generatedAt does not match the provided snapshot")
		}
		if supplied.HistoryID != "" && (snapshot.History == nil || snapshot.History.ID != supplied.HistoryID) {
			return errors.New("request historyId does not match the provided snapshot")
		}
		request = supplied.ProposalRequest
	}
	proposal, err := review.Propose(snapshot, request)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	switch *format {
	case "json":
		enc := json.NewEncoder(&body)
		enc.SetIndent("", "  ")
		err = enc.Encode(proposal)
	case "bundle":
		err = review.Bundle(&body, proposal)
	case "markdown", "flows":
		name := "proposal.md"
		if *format == "flows" {
			name = "flows.yaml"
		}
		for _, file := range proposal.Files {
			if file.Name == name {
				body.WriteString(file.Content)
				break
			}
		}
	}
	if err != nil {
		return err
	}
	if *output == "" {
		_, err = w.Write(body.Bytes())
		return err
	}
	f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating proposal output: %w", err)
	}
	_, writeErr := f.Write(body.Bytes())
	return errors.Join(writeErr, f.Close())
}
