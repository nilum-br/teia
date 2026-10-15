package review

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/nilum-br/teia/internal/model"
)

// ProposalRequest identifies one observed addition and the owner's rationale.
// It never accepts replacement evidence or an arbitrary repository path.
type ProposalRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Rationale string `json:"rationale"`
	InputKind string `json:"-"` // Set by the trusted caller; never accepted over HTTP.
}

type ProposalSource struct {
	SnapshotID  string    `json:"snapshotId"`
	InputKind   string    `json:"inputKind"`
	HistoryID   string    `json:"historyId,omitempty"`
	GeneratedAt time.Time `json:"generatedAt"`
	Round       int64     `json:"round"`
	TimeMode    string    `json:"timeMode"`
}

type EndpointIdentity struct {
	ID         string                     `json:"id"`
	Ref        string                     `json:"ref"`
	Kind       model.NodeKind             `json:"kind"`
	Tech       string                     `json:"tech,omitempty"`
	Namespace  string                     `json:"namespace,omitempty"`
	Resolution []model.IdentityResolution `json:"resolution,omitempty"`
}

type ProposalConnection struct {
	From       string                    `json:"from"`
	To         string                    `json:"to"`
	Protocol   string                    `json:"protocol,omitempty"`
	Port       int                       `json:"port,omitempty"`
	Identity   string                    `json:"identity"`
	Resolution *model.ConnectionIdentity `json:"resolution,omitempty"`
	Endpoints  []EndpointIdentity        `json:"endpoints"`
	Sources    []string                  `json:"sources"`
	Signals    []string                  `json:"signals"`
	Evidence   []model.Evidence          `json:"evidence"`
}

type ProposalFile struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Content   string `json:"content"`
}

// Proposal is an immutable review artifact, not an accepted declaration or PR.
type Proposal struct {
	Version    int                `json:"version"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Rationale  string             `json:"rationale"`
	Source     ProposalSource     `json:"source"`
	Connection ProposalConnection `json:"connection"`
	Advice     []string           `json:"advice"`
	Files      []ProposalFile     `json:"files,omitempty"`
}

// Propose reads a snapshot already held by the caller. Only undeclared,
// observed communications with stable endpoint identities can become fragments.
func Propose(snapshot model.Snapshot, request ProposalRequest) (Proposal, error) {
	var p Proposal
	if request.InputKind == "" {
		request.InputKind = "provided-snapshot"
	}
	if request.InputKind != "provided-snapshot" && request.InputKind != "current-discovery" && request.InputKind != "stored-history" {
		return p, errors.New("unknown proposal input kind")
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return p, err
	}
	if snapshot.GeneratedAt.IsZero() {
		return p, errors.New("proposal requires a snapshot capture time")
	}
	for _, id := range []string{request.From, request.To} {
		if strings.TrimSpace(id) == "" || len(id) > 1024 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return p, errors.New("from and to must be nonempty identities without control characters (at most 1024 bytes)")
		}
	}
	if request.From == request.To {
		return p, errors.New("self communication is not an additive contract proposal")
	}
	request.Rationale = strings.TrimSpace(request.Rationale)
	if request.Rationale == "" || len(request.Rationale) > 4096 || strings.IndexFunc(request.Rationale, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return p, errors.New("provide an owning-team rationale between 1 and 4096 bytes without control characters")
	}
	var edge *model.Edge
	for i := range snapshot.Edges {
		if snapshot.Edges[i].From == request.From && snapshot.Edges[i].To == request.To {
			edge = &snapshot.Edges[i]
			break
		}
	}
	if edge == nil || !edge.Observed {
		return p, errors.New("communication was not observed in the selected snapshot")
	}
	if edge.Declared {
		return p, errors.New("communication is already declared; review the owning file instead of adding a duplicate")
	}
	nodeMap := nodes(snapshot)
	for _, id := range []string{edge.From, edge.To} {
		if _, ok := nodeMap[id]; !ok {
			return p, fmt.Errorf("endpoint %q has no identity record in the selected snapshot", id)
		}
	}
	candidate := Candidate{ID: edge.Key(), Action: "add", Edge: *edge, Identity: identity(snapshot, *edge)}
	var yaml bytes.Buffer
	if err := Flows(&yaml, Report{Mode: "contract", Candidates: []Candidate{candidate}}, snapshot, map[string]string{edge.Key(): request.Rationale}); err != nil {
		return p, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return p, fmt.Errorf("snapshot cannot be fingerprinted: %w", err)
	}
	p = Proposal{Version: 1, Title: "Declare communication: " + edge.From + " -> " + edge.To, Rationale: request.Rationale,
		Source: ProposalSource{SnapshotID: digest(data), InputKind: request.InputKind, GeneratedAt: snapshot.GeneratedAt, Round: snapshot.Round, TimeMode: snapshot.TimeMode},
		Connection: ProposalConnection{From: edge.From, To: edge.To, Protocol: edge.Protocol, Port: edge.Port, Identity: candidate.Identity,
			Sources: append([]string{}, edge.Sources...), Signals: append([]string{}, edge.Signals...), Evidence: append([]model.Evidence{}, edge.Evidence...)},
		Advice: []string{
			"Review endpoint identities, aliases and ownership before changing the contract.",
			"The selected snapshot's declared flag reflects configured discovery; compare the current owning repository to catch declarations it did not load or changes made since capture.",
			"Merge only flows.yaml spec.egress into the owner's existing Flows document; preserve unrelated flows and resolve protocol/port conflicts instead of appending duplicates.",
			"Run teia validate on the resulting file and review git diff before committing. This fragment is not a patch that can be applied blindly.",
			"The snapshot fingerprint identifies input content; it does not guarantee archive retention. Preserve proposal.json and its evidence for review.",
			"This proposal does not approve traffic, change discovery, deploy policy, or open a pull request. No network calls or repository changes were made.",
		}}
	if snapshot.History != nil {
		p.Source.HistoryID = snapshot.History.ID
	}
	if edge.Identity != nil {
		p.Connection.Resolution = &model.ConnectionIdentity{From: copyResolutions(edge.Identity.From), To: copyResolutions(edge.Identity.To)}
	}
	for _, id := range []string{edge.From, edge.To} {
		n := nodeMap[id]
		namespace := n.Labels["k8s.namespace"]
		if namespace == "" {
			namespace = n.Labels["namespace"]
		}
		p.Connection.Endpoints = append(p.Connection.Endpoints, EndpointIdentity{ID: id, Ref: reviewRef(n, id), Kind: n.Kind, Tech: n.Tech, Namespace: namespace, Resolution: copyResolutions(n.Identity)})
	}
	if len(edge.Evidence) == 0 {
		p.Advice = append(p.Advice, "Original signal timing was not supplied; snapshot capture time is not call time.")
	}
	for _, ev := range edge.Evidence {
		if ev.Retained {
			p.Advice = append(p.Advice, "Evidence includes retained observations; this is historical context, not proof of current traffic.")
			break
		}
	}
	// The identity depends on the complete source and rationale, not wall-clock
	// generation time. Files do not recursively include their own serialized copy.
	data, err = json.Marshal(p)
	if err != nil {
		return Proposal{}, err
	}
	p.ID = digest(data)
	metadata, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return Proposal{}, err
	}
	markdown := proposalMarkdown(p, string(metadata), yaml.String())
	p.Files = []ProposalFile{{Name: "flows.yaml", MediaType: "application/yaml", Content: yaml.String()},
		{Name: "proposal.md", MediaType: "text/markdown", Content: markdown},
		{Name: "proposal.json", MediaType: "application/json", Content: string(metadata) + "\n"}}
	return p, nil
}

func copyResolutions(input []model.IdentityResolution) []model.IdentityResolution {
	if input == nil {
		return nil
	}
	result := append([]model.IdentityResolution{}, input...)
	for i := range result {
		result[i].Candidates = append([]model.IdentityCandidate(nil), input[i].Candidates...)
		result[i].Alternatives = append([]string(nil), input[i].Alternatives...)
	}
	return result
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func proposalMarkdown(p Proposal, metadata, yaml string) string {
	var out strings.Builder
	out.WriteString("# Communication contract proposal\n\nThis is a proposed addition for owning-team review. It has not been applied.\n\n## Rationale\n\n")
	writeFence(&out, "text", p.Rationale)
	out.WriteString("\n## Validated additive Flows fragment\n\n")
	writeFence(&out, "yaml", yaml)
	out.WriteString("\n## Review and merge\n\n")
	for _, advice := range p.Advice {
		out.WriteString("- " + advice + "\n")
	}
	out.WriteString("\n## Optional Git handoff\n\nThe operator chooses the owning repository, base branch and actual file path. Extract the fragment outside the repository, then merge its single egress entry into the existing owner document. The following commands are examples to review and execute explicitly; Teia does not execute them.\n\n")
	writeFence(&out, "sh", "git switch -c teia/accept-communication\n# Merge the reviewed entry into the owning file before continuing.\nteia validate path/to/flows.yaml\ngit diff --check\ngit diff -- path/to/flows.yaml\n")
	out.WriteString("\nOnly after reviewing that diff and obtaining the repository's required approval:\n\n")
	writeFence(&out, "sh", "git add -- path/to/flows.yaml\ngit commit -m \"Declare reviewed communication\"\ngit push -u origin teia/accept-communication\ngh pr create --draft --repo OWNER/REPOSITORY --base main --head teia/accept-communication --title \"Declare reviewed communication\" --body-file /path/to/proposal.md\n")
	out.WriteString("\nThe push and gh command publish to the explicitly selected repository. Authenticate gh locally; no credential belongs in this proposal or the browser. GitHub's /compare page is an alternative after the branch has been pushed. A draft PR is still publication; gh --dry-run may also push changes and is not used for local preview.\n\nReferences: https://cli.github.com/manual/gh_pr_create and https://docs.github.com/en/pull-requests/how-tos/commit-changes/comparing-commits .\n")
	out.WriteString("\n## Source and identity\n\nCapture time is separate from original signal times. Missing timestamps remain unknown.\n\n")
	writeFence(&out, "json", metadata)
	return out.String()
}

func writeFence(out *strings.Builder, language, content string) {
	fence := "```"
	for strings.Contains(content, fence) {
		fence += "`"
	}
	out.WriteString(fence + language + "\n" + content)
	if !strings.HasSuffix(content, "\n") {
		out.WriteByte('\n')
	}
	out.WriteString(fence + "\n")
}

// Bundle writes the generated files to a deterministic ZIP. Entry names are
// fixed by Propose, never derived from service IDs or client repository paths.
func Bundle(w io.Writer, p Proposal) error {
	z := zip.NewWriter(w)
	for _, file := range p.Files {
		if file.Name != "flows.yaml" && file.Name != "proposal.md" && file.Name != "proposal.json" {
			return errors.New("unexpected proposal file name")
		}
		entry, err := z.Create(file.Name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(entry, file.Content); err != nil {
			return err
		}
	}
	return z.Close()
}
