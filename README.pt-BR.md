<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="teia" width="260">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/nilum-br/teia/actions/workflows/ci.yml"><img src="https://github.com/nilum-br/teia/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-005aff" alt="Apache-2.0"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/nilum-br/teia?color=005aff" alt="Versão do Go"></a>
  <a href="plugins/grafana"><img src="https://img.shields.io/badge/Grafana-painel-a9f549?labelColor=020f59" alt="Painel do Grafana"></a>
</p>

**Seu diagrama de arquitetura está mentindo em algum lugar. A teia mostra onde, e o que fazer.**

A teia mapeia quem fala com quem na sua plataforma a partir da telemetria que você **já tem**: traces, métricas
de mesh, fluxos eBPF, logs de acesso e os próprios bancos. Depois compara com o que os times *declararam*. E,
diferente de um mapa de serviços comum, diz exatamente onde está cega e qual permissão, adaptador ou
instrumentação faria ela enxergar mais. Roda como um binário só em Linux, macOS e Windows, e aparece onde o time
já olha: no **Grafana**.

[English](README.md)

![O mapa da teia no lab: serviços, bancos e jobs com saúde, tráfego ao vivo, drift e os próximos passos para enxergar mais](docs/assets/screenshots/teia-map.png)

## Por quê

Mapas de serviços já existem no Grafana, Kiali, Hubble e nos APMs. A teia reúne três perguntas:

- **Drift**: que comunicações acontecem sem ninguém ter declarado, e quais declaradas nunca acontecem?
- **Cobertura**: quanto do quadro eu **não** estou vendo, e o que exatamente resolve?
- **Contrato**: onde está a lista revisada de "A pode chamar B na porta X, porque Y"?

O **advisor** vai "fiando a teia": a cada rodada aponta o próximo ponto cego e o passo concreto (o `GRANT`, o RBAC,
a instrumentação) para enxergá-lo.

## Agnóstica de propósito

- **Não precisa de service mesh nem de agente novo.** Mesh é um sinal entre vários; veja [docs/sources.md](docs/sources.md).
- **Detecção automática:** apontada para o Prometheus, encontra sozinha métricas de service graph, Istio, Linkerd, Beyla/OBI, Hubble e Retina (10 presets). O `teia detect` mostra o que a plataforma oferece e gera um `teia.yaml` inicial.
- **Qualquer outra coisa** vira aresta com uma query PromQL ou LogQL e um mapeamento de labels.
- **Ambiente misto é o normal:** parte com Istio, parte com OpenTelemetry, parte sem nada. Cada aresta registra quais sinais a viram.
- **Identidade confiável:** IPs são resolvidos pelo inventário, e IP reciclado nunca é atribuído ao workload errado.

## O que você vê

O mapa, no Grafana (`nilum-teia-panel`) ou na interface da própria teia, é o mesmo componente:

![O painel da teia dentro de um dashboard do Grafana](docs/assets/screenshots/grafana-panel.png)

- **Sinais de ouro, e o porquê:** cada card e cada linha mostram tráfego, latência p95 e taxa de erro. Amarelo ou vermelho é o pior sinal acima do limite, e uma etiqueta no card ou na linha diz qual é e de onde provavelmente vem: você vê a causa de relance, não só a cor (`p95 900 ms from inventory`, `4.4% errors`, `3 on locks held by legacy-reports`, `1.5k messages waiting`, `7 min behind`, `93% memory`).
- **Todo tipo de componente:** serviços (erros, latência, CPU estrangulada, memória, restarts), bancos relacionais (conexões, sessões esperando lock e quem segura), MongoDB, Redis (memória, clientes) e filas (lag do Kafka em mensagens e em tempo, consumidor sem progresso, backlog do RabbitMQ), lidos dos exporters que você já roda.
- **Tráfego andando:** pontos percorrem cada comunicação na velocidade do tráfego; pontos vermelhos são erros. Ao vivo.
- **Drift e visibilidade:** tracejado é não declarado, pontilhado é declarado e não visto; a borda do card diz o quanto a teia vê em primeira mão.
- **[Biblioteca de ícones](docs/icons.md) padronizada:** microsserviço, banco relacional, banco de documentos (NoSQL), cache, stream (Kafka), fila, gateway, job, SaaS externo. A tecnologia é detectada pelos spans, pela imagem ou por label.
- **Jornadas e filtros:** mostre uma jornada sozinha, num quadro, com portas por onde ela entra e por onde sai, e por que está vermelha. O teia acha a label de jornada que seus serviços já têm (`journey`, `app.kubernetes.io/part-of` ou `system`). O mesmo Filtro recorta o mapa por qualquer label, saúde, tipo de componente ou tecnologia. Veja [docs/journeys.md](docs/journeys.md).
- **Valores nas linhas:** cada linha diz o tráfego e o p95 (`1.8/s · 9.5 ms`); uma linha amarela ou vermelha diz o porquê no lugar. `V` liga e desliga.
- **Labels:** agrupe por `team`, `system`, `k8s.namespace` ou qualquer label; busque com `team:checkout`, `tech:kafka`, `journey=checkout`.
- **Próximos passos, feed ao vivo e exportação** (PNG, SVG, matriz de fluxos em CSV para pedido de firewall).
- **Teclado e leitor de tela:** todo card e controle é alcançável com Tab; `F` enquadra, `/` busca, `J` abre os filtros, `V` liga os valores nas linhas e `Espaço` liga e desliga a animação, que também segue a preferência de movimento reduzido.

| Amarelo ou vermelho diz o porquê, e de onde vem | Agrupe por qualquer label, no tema escuro ou claro |
|---|---|
| ![recommendations está amarelo: o p95 vem do inventory, com um botão para ir até lá](docs/assets/screenshots/teia-details.png) | ![O lab agrupado por time, tema claro](docs/assets/screenshots/teia-groups-light.png) |
| **A fila diz quanto espera e quão atrasada está** | **O cache ou o banco diz o quanto está cheio** |
| ![email-worker para order-events está amarelo: 1,5 mil mensagens esperando, 75 s de atraso, do kafka_exporter](docs/assets/screenshots/teia-queue.png) | ![catalog-cache: memória contra o maxmemory e clientes contra o maxclients, do redis_exporter](docs/assets/screenshots/teia-cache-light.png) |

As visões viram links compartilháveis: `?journey=checkout`, `?filter=k8s.namespace:shop,health:failing`, `?group=team`, `?q=kind:database`, `?select=orders-db`, `?theme=light`, `?flow=1`, `?values=0`, `?legend=icons`.

![A jornada checkout no seu quadro, com os bancos e gateways numa faixa à direita, uma porta onde ela sai para browse, e o resumo: orders → payments, erros](docs/assets/screenshots/teia-journey.png)

<p align="center"><img src="docs/assets/icon-library.svg" alt="A biblioteca de ícones da teia: um pictograma por tipo de componente" width="820"></p>

As mudanças também saem por **webhook** (JSON ou Slack) e **métricas Prometheus** (`/metrics`) para alerta.

## Como funciona

<p align="center"><img src="docs/assets/architecture.svg" alt="As fontes que a teia lê, a rodada de descoberta e onde o resultado aparece" width="900"></p>

A teia só lê e não guarda banco de dados: a cada intervalo pergunta a cada fonte o que ela viu, junta as respostas
num grafo com identidades estáveis, compara com os flows declarados, calcula saúde e cobertura e pergunta ao
advisor o que deixaria o quadro mais completo. O resultado sai como JSON, stream ao vivo, métricas e webhooks.

## Testar em 5 minutos

Só precisa de Docker (Linux, macOS ou Windows). O lab é uma pequena loja com a mistura que as plataformas reais têm
(parte OpenTelemetry, parte mesh Istio, Kafka e MongoDB gerenciados, Redis, PostgreSQL e jobs sem instrumentação) e
um problema de cada tipo para o mapa explicar: o payments falha chamadas, o inventory responde devagar, um
consumidor está atrasado e o teste ponta a ponta trava uma tabela.

```sh
docker compose -f lab/compose.yaml up -d --build
```

| O quê | Onde |
|---|---|
| **Grafana com o painel da teia** | http://localhost:3480/d/teia-overview |
| Interface da teia | http://localhost:8480 |
| Prometheus | http://localhost:9480 |

```sh
docker compose -f lab/compose.yaml exec teia teia discover          # o que a teia vê e o que falta
docker compose -f lab/compose.yaml exec orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'
docker compose -f lab/compose.yaml exec teia teia discover          # o job legado escondido aparece
docker compose -f lab/compose.yaml exec teia teia export matrix --lang pt-BR --declared-only
docker compose -f lab/compose.yaml run --rm e2e                     # o teste ponta a ponta que o CI roda
```

Atrás de proxy com inspeção TLS? Coloque a CA como `.crt` em `certs/` antes do build.

## Usar no seu Grafana

Quatro passos, sem mudar nada nos seus serviços. A teia lê o Prometheus, o Tempo e o Loki que você já tem. O painel
acha jornadas, times e namespaces nas labels que seus workloads já carregam.

**1. Rode a teia onde ela alcança sua telemetria.** O `teia detect` testa o que você passar e gera uma configuração
inicial:

```sh
docker build -t teia .     # as releases vão publicar ghcr.io/nilum-br/teia
docker run --rm teia detect --prometheus http://prometheus:9090 --tempo http://tempo:3200 --loki http://loki:3100 > teia.yaml
docker run -d --name teia -p 8480:8080 -v "$PWD/teia.yaml:/etc/teia/teia.yaml:ro" teia
```

Adicione `--kubernetes` ou `--docker` ao `detect` para a teia conhecer também os workloads que não emitem nada.
Abra http://localhost:8480: se o mapa aparece, a teia está lendo sua telemetria.

**Vai rodar no Kubernetes?** Siga o [guia de instalação com Helm](docs/kubernetes.md): até a primeira release,
faça o build e envie a imagem ao seu registry, crie um Secret para o token da API e configure sua telemetria
em `config.sources` no arquivo `my-values.yaml`:

```sh
helm upgrade --install teia ./charts/teia -n teia --create-namespace -f my-values.yaml
helm test teia -n teia
```

O chart sobe um pod só de leitura com permissão para consultar o inventário. Não instala operator nem CRD.
O Grafana no mesmo cluster pode usar `http://teia.teia.svc:8080` no passo 3. O guia detalha permissões, arquivos
de flows, troca de token e as opções de Ingress e ServiceMonitor.

**2. Instale o plugin do painel** (`nilum-teia-panel`). Até ele ser assinado e entrar no catálogo, faça o build e
carregue como plugin não assinado:

```sh
docker run --rm --user "$(id -u):$(id -g)" -e npm_config_cache=/tmp/npm -v "$PWD:/repo" -w /repo/plugins/grafana node:24 sh -c "npm ci && npm run build"
cp -r plugins/grafana/dist /var/lib/grafana/plugins/nilum-teia-panel
```

```ini
# grafana.ini (ou GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=nilum-teia-panel)
[plugins]
allow_loading_unsigned_plugins = nilum-teia-panel
```

Reinicie o Grafana. Com o Helm chart, monte o `dist/` no mesmo caminho ou, a partir da primeira release, liste o zip
em `plugins:`.

**3. Crie um data source que lê a teia.** Instale o data source
[Infinity](https://grafana.com/grafana/plugins/yesoreyeram-infinity-datasource/) e informe o endereço da teia uma
vez só, aqui. Quem chama a teia é o servidor do Grafana, então use um endereço que ele alcance:

```yaml
# provisioning/datasources/teia.yaml
apiVersion: 1
datasources:
  - name: teia
    uid: teia                      # o dashboard procura esse uid
    type: yesoreyeram-infinity-datasource
    access: proxy
    url: http://teia:8080          # a sua teia
    jsonData:
      allowedHosts: ["http://teia:8080"]
      # com auth.token na teia:
      # httpHeaderName1: Authorization
    # secureJsonData:
    #   httpHeaderValue1: Bearer ${TEIA_TOKEN}
```

**4. Importe o dashboard.** No Grafana, vá em Dashboards → New → Import e envie o
[`lab/grafana/dashboards/teia.json`](lab/grafana/dashboards/teia.json). As queries dele só têm caminhos, então ele
funciona com qualquer teia para a qual o data source aponte.

Pronto. O **Filtro** do painel lista suas jornadas, namespaces, times, saúde e tecnologias a partir dos próprios
dados. As opções do painel definem onde ele abre:

- **Journey** aceita um valor ou uma variável, como `${journey}`; links como `?var-journey=checkout` funcionam.
- **Journey label** indica outra label, quando a sua não é `journey`, `app.kubernetes.io/part-of` nem `system`.
- **Group by label** define o agrupamento inicial.

Mais em [plugins/grafana](plugins/grafana/README.md), inclusive um modo direto que faz streaming da teia sem
queries.

Refez o build do plugin sem mudar a versão e o Grafana segue mostrando o painel antigo? O Grafana deixa o navegador
guardar os arquivos do plugin por uma hora sob a versão: recarregue com Ctrl+Shift+R. As releases mudam a versão,
então isso só acontece com builds locais. O lab carimba cada build com uma versão própria.

## Instalar

- **Pelo código-fonte** (hoje): `go build ./cmd/teia` (Go 1.27+).
- **Releases** (a partir da primeira tag): binários para Linux, macOS e Windows (amd64, arm64), imagem multi-arch em `ghcr.io/nilum-br/teia` e o zip do painel do Grafana.
- **Kubernetes:** [chart Helm e guia de instalação](docs/kubernetes.md). As releases também empacotam o chart e o
  publicam em `oci://ghcr.io/nilum-br/charts/teia`, com a mesma versão da imagem.

Depois, `teia detect` mostra o que a plataforma oferece e gera um `teia.yaml` inicial, e `teia serve` sobe a API e
a interface. Antes de expor a API além do localhost, configure `auth.token` (e `cors.allowedOrigins` para o modo
direto do painel do Grafana): veja [Configuração](docs/configuration.md) e [Expondo a API](docs/architecture.md#exposing-the-api).

## Declarar flows

`Flows` é um **formato de arquivo, não um CRD do Kubernetes**. O `apiVersion: teia.nilum.io/v1alpha1` e o
`kind: Flows` identificam esse formato, como ocorre no `catalog-info.yaml` do Backstage. Não use `kubectl apply`
nesse arquivo: não existe um recurso `Flows` instalado no cluster.

Guarde o arquivo no repositório do serviço. O chart monta os arquivos de `flows` ou `flowsConfigMap`, e a teia
relê as alterações a cada rodada, sem reiniciar. O advisor gera um arquivo inicial a partir das comunicações
observadas para o time revisar. Veja a [especificação](docs/flows-spec.md) e
[como fornecer os arquivos no Kubernetes](docs/kubernetes.md#flows-files-not-a-crd).

## Documentação

[Sinais e fontes](docs/sources.md) · [Configuração](docs/configuration.md) · [Kubernetes / Helm](docs/kubernetes.md) · [Arquitetura](docs/architecture.md) ·
[Jornadas](docs/journeys.md) · [Regras do advisor](docs/advisor.md) · [Ícones](docs/icons.md) · [Especificação dos flows](docs/flows-spec.md) ·
[Roadmap](docs/roadmap.md) · [Panorama](docs/landscape.md)

## Contribuir e licença

Issues e PRs são bem-vindos; veja o [CONTRIBUTING.md](CONTRIBUTING.md). Vulnerabilidades devem ser reportadas em
privado, como descrito no [SECURITY.md](SECURITY.md). Apache-2.0. Feito pela [Nilum](https://nilum.com.br) e contribuidores.
