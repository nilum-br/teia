# Entrega técnica do Teia — T01–T16

Estado em **15 de outubro de 2026**: implementação e validação técnica prontas para o colega avaliar.
O escopo é T01–T16; Backstage, geração de políticas de rede e data source próprio permanecem fora desta entrega.
O [roadmap](roadmap.md) registra cada critério e os limites reais.

Por decisão do responsável, o colega organizará o piloto humano de T12 após esta entrega. A organização
Grafana, aprovação de assinatura pública, catálogo e publicação assinada de T10 também ficam para depois.
O pacote entregue é uma **versão candidata sem assinatura**, validada em ambiente de teste; esses critérios
externos não foram marcados como executados.

## O que foi entregue

- Dados ausentes permanecem desconhecidos; percentis agregados são qualificados; horário da coleta não
  substitui horário da observação. Os mesmos contratos passam pela API, standalone e Grafana.
- Investigação por componente/conexão, filtros e compartilhamento, teclado, hierarquia, foco legível,
  proveniência dos thresholds e links reais para Tempo, Loki e dashboards.
- Jornadas com múltiplos pertencimentos, limites e fronteiras explícitas, sem inferir transações de rótulos.
- Histórico persistente opcional com retenção/tamanho limitados, recuperação após reinício e replay por ID,
  inclusive no Grafana. É um arquivo de capturas discretas, sem interpolar intervalos não observados.
- `teia diff`, fragmentos Flows aceitos explicitamente e frequência esperada para jobs raros.
- Diagnósticos adicionais de Redis, Kafka, RabbitMQ e PostgreSQL, com pré-requisitos e capacidades ausentes
  visíveis. Explorers adicionais foram avaliados; não são apresentados como implementados.
- Dependências diretas nas versões estáveis verificadas em outubro de 2026, npm com zero alertas e scans Go
  sem vulnerabilidades conhecidas. APIs internas antigas de SDKs atuais têm motivos e condições de migração
  registrados em [dependency-status.md](dependency-status.md).

## Abrir nesta máquina

| Ambiente | Endereço |
|---|---|
| Teia standalone | <http://localhost:8480> |
| Dashboard completo | <http://localhost:3480/d/teia-overview/teia> |
| Dashboard de histórico | <http://localhost:3480/d/teia-history/teia> — preencher a variável `snapshot` |
| Candidato isolado, Grafana 13.2.4 | <http://localhost:3484/d/teia-candidate-sample> |
| Comparador Novatec instalado | <http://localhost:3481/d/teia-novatec-comparison> |

O Grafana do lab usa Editor anônimo apenas na interface local para permitir Explore. Não copiar essa
configuração de acesso para produção. Os links de evidência usam a identidade e o intervalo disponíveis;
uma busca por serviço/tempo não identifica necessariamente uma chamada específica.

## Reproduzir a instalação

No checkout do repositório, com Docker disponível:

```powershell
docker compose -f lab/compose.yaml up -d --build
docker compose -f lab/compose.yaml run --rm e2e
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --build --rm visual
```

O primeiro comando também constrói o painel. Aguarde os serviços ficarem prontos. Em rede com inspeção TLS,
use a CA corporativa conforme [CONTRIBUTING](../CONTRIBUTING.md#troubleshooting), sem desativar TLS.
O teste E2E exercita o cenário de permissões/locks do banco e restaura sua concessão temporária.

Pacote do painel nesta máquina:

```text
dist/candidate/0.1.0-candidate.20261015.4/nilum-teia-panel-0.1.0-candidate.20261015.4.zip
SHA256 4db7786712341351debf453b580b25487cbddb693e9e1524a29529d98fa362f4
```

O diretório `dist` é local e ignorado pelo Git. Transfira o ZIP junto com `checksums.txt`, ou gere outra versão
seguindo [installation-candidate.md](installation-candidate.md). Não atribua esse hash a um rebuild diferente.
A instalação de teste precisa permitir especificamente `nilum-teia-panel` sem assinatura. O guia também traz
provisioning, dados mínimos, instalação Helm e os passos para assinatura/publicação futura.

## Evidências de validação

| Área | Resultado |
|---|---|
| Backend | `go test -race ./...`, `go vet`, golangci-lint e tidy limpos; PromQL validado com promtool e PostgreSQL real |
| UI/painel | 79 testes core, 53 Jest e 2 integrações de dependências; TypeScript/lint/build passaram |
| Navegador/API | 43 casos na suíte final, mais regressão de foco legível com 1.000 nós; Tempo/Loki/dashboard reais |
| Cenários | Sete casos × duas interfaces, incluídos na suíte: erros, latência, fila, fonte indisponível, serviço compartilhado, ghost e unknown |
| Escala | 100/500/1.000 nós, até 1.800 conexões; p95 de seleção/filtro abaixo de 200 ms na máquina de referência |
| Instalação | Grafana 13.2.0 limpo → 13.2.4 na mesma base, pacote idêntico; Helm com autenticação, SA/RBAC externo e histórico preservado |
| Mercado | Novatec 4.2.0 instalado e assinado; diferenças observadas documentadas. Kiali avaliado documentalmente, sem ensaio instalado neste lab |

Os detalhes e ressalvas estão em [UI validation](ui-validation.md),
[competitive validation](competitive-validation.md), [resource diagnostics](resource-diagnostics.md) e
[contract review](contract-review.md). A execução de CI correspondente pode ser consultada no histórico
do commit entregue em [GitHub Actions](https://github.com/nilum-br/teia/actions/workflows/ci.yml).

## Próximos passos já combinados

1. O colega segue [usability-pilot.md](usability-pilot.md) com cinco participantes e preenche
   [pilot-results.csv](pilot-results.csv). Hoje a planilha não contém resultados humanos.
2. O responsável define a organização Grafana e obtém a aprovação de assinatura. O secret é configurado no
   repositório; tokens não entram em relatórios, dashboards ou Git.
3. Após esses critérios, publicar e baixar os artefatos assinados para repetir instalação/upgrade sem exceção
   de plugin unsigned. Execuções manuais do workflow geram candidatos; publicação exige push de tag e assinatura.

As melhorias de contrato, evidência e dados desconhecidos atendem ao problema principal do Teia. A comparação
não sustenta uma afirmação de superioridade universal nem substitui o piloto humano.
