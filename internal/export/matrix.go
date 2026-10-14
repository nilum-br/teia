// Package export renders a snapshot for people outside the platform team.
//
// The flow matrix is the spreadsheet security and network teams ask for
// before opening firewall rules (in Brazil often called "matriz de
// comunicação" or "matriz de fluxos"): one row per communication, with the
// addresses teia knows and the reason teams declared.
package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nilum-br/teia/internal/model"
)

// MatrixOptions selects rows and header language.
type MatrixOptions struct {
	// DeclaredOnly keeps only intended communication, which is what a
	// firewall request should be based on.
	DeclaredOnly bool
	// Lang is "en" (default) or "pt-BR".
	Lang string
}

var headers = map[string][]string{
	"en": {"source", "source_addresses", "destination", "destination_addresses", "destination_kind",
		"protocol", "port", "reason", "state", "declared", "observed", "requests_per_second", "evidence"},
	"pt-BR": {"origem", "enderecos_origem", "destino", "enderecos_destino", "tipo_destino",
		"protocolo", "porta", "justificativa", "situacao", "declarado", "observado", "requisicoes_por_segundo", "evidencias"},
}

var stateLabel = map[string]map[model.EdgeState]string{
	"en":    {model.EdgeOK: "ok", model.EdgeGhost: "undeclared", model.EdgeStale: "not observed"},
	"pt-BR": {model.EdgeOK: "ok", model.EdgeGhost: "nao declarado", model.EdgeStale: "nao observado"},
}

// Matrix writes the flow matrix as CSV.
func Matrix(w io.Writer, s model.Snapshot, opts MatrixOptions) error {
	lang := opts.Lang
	if _, ok := headers[lang]; !ok {
		lang = "en"
	}
	nodes := map[string]model.Node{}
	for _, n := range s.Nodes {
		nodes[n.ID] = n
	}
	yes, no := "yes", "no"
	if lang == "pt-BR" {
		yes, no = "sim", "nao"
	}
	flag := func(b bool) string {
		if b {
			return yes
		}
		return no
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(headers[lang]); err != nil {
		return err
	}
	for _, e := range s.Edges {
		if opts.DeclaredOnly && !e.Declared {
			continue
		}
		port := ""
		if e.Port > 0 {
			port = strconv.Itoa(e.Port)
		}
		rate := ""
		if e.RequestRate > 0 {
			rate = fmt.Sprintf("%.2f", e.RequestRate)
		}
		row := []string{
			e.From, strings.Join(nodes[e.From].Addresses, " "),
			e.To, strings.Join(nodes[e.To].Addresses, " "), string(nodes[e.To].Kind),
			e.Protocol, port, e.Reason, stateLabel[lang][e.State],
			flag(e.Declared), flag(e.Observed), rate, strings.Join(e.Sources, " "),
		}
		for i := range row {
			row[i] = cell(row[i])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// formulaStart lists what makes a spreadsheet read a cell as a formula.
const formulaStart = "=+-@\t\r"

// cell keeps a value inert in a spreadsheet: names, reasons and addresses
// come from telemetry and flows files, and "=HYPERLINK(...)" in one would
// run when a reviewer opens the matrix. A leading quote makes it text.
func cell(v string) string {
	if v != "" && strings.ContainsRune(formulaStart, rune(v[0])) {
		return "'" + v
	}
	return v
}
