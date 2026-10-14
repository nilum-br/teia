# teia

**Seu diagrama de arquitetura está mentindo em algum lugar. A teia mostra onde, e o que fazer.**

A teia mapeia quem fala com quem na sua plataforma e compara com o que os times *declararam*. E,
diferente de um mapa de serviços comum, diz exatamente onde está cega e qual permissão, adaptador ou
instrumentação faria ela enxergar mais.

[English](README.md)

## Por quê

Mapa de serviços virou commodity: Grafana, Kiali, Hubble e qualquer APM desenham um. Nenhum deles
responde:

- **Drift**: que comunicações acontecem sem ninguém ter declarado, e quais declaradas nunca acontecem?
- **Cobertura**: quanto do quadro eu **não** estou vendo, e o que exatamente resolve?
- **Contrato**: onde está a lista revisada e versionada de "o serviço A pode chamar o B na porta X, porque Y"?
  É dela que segurança, rede e plataforma precisam para trabalhar.

A teia junta três camadas num grafo só:

| Camada | De onde vem |
|---|---|
| **Declarado** (intenção) | arquivos `Flows` ao lado do `catalog-info.yaml` de cada serviço |
| **Observado** (realidade) | adaptadores sobre o que você já roda: service graph do OpenTelemetry/Tempo, inventário, os próprios bancos |
| **Drift** | a diferença. *ghost*: observado e não declarado. *stale*: declarado e não observado |

Por cima disso, o **advisor** vai "fiando" a teia: a cada rodada aponta o próximo ponto cego e o passo
concreto para enxergá-lo.

```text
 1. [warning/permission] Reached database orders-db but cannot see who else connects to it
    Right now 2 session(s) are hidden, from roles: orders (1), reports (1).
    -> GRANT pg_monitor TO "teia";
```

Você aplica, e na rodada seguinte aparece o job legado que ninguém instrumentou, já com o próximo passo
para ver dentro dele.

## Testar em 5 minutos

Só precisa de Docker:

```sh
docker compose -f lab/compose.yaml up -d --build
```

| O quê | Onde |
|---|---|
| UI da teia (grafo, próximos passos, export) | http://localhost:8480 |
| Grafana (Node Graph + tabela de achados) | http://localhost:3480 |
| Prometheus | http://localhost:9480 |

O roteiro do lab:

```sh
# 1. o que a teia vê, e o que falta?
docker compose -f lab/compose.yaml exec teia teia discover

# 2. faça o que o primeiro achado pede
docker compose -f lab/compose.yaml exec orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'

# 3. de novo: aparece o legacy-reports -> orders-db, e a visibilidade sobe de 50% para 63%
docker compose -f lab/compose.yaml exec teia teia discover

# matriz de fluxos para pedido de liberação de firewall (CSV com cabeçalho em português)
docker compose -f lab/compose.yaml exec teia teia export matrix --lang pt-BR --declared-only
```

Atrás de proxy com inspeção TLS? Coloque a CA como `.crt` em `certs/` antes do build.

## Estado

**v0, esqueleto que funciona.**

- engine em Go: merge, resolução de identidade, drift, cobertura;
- fontes: declarado, service graph (Prometheus), Docker e explorador Postgres;
- advisor com IDs estáveis e correções prontas para copiar;
- CLI, API, UI embutida, Grafana e lab.

Próximos passos: inventário Kubernetes, plugin Backstage (card + relações no catálogo) e `teia diff`
comentando em PR. Veja [docs/roadmap.md](docs/roadmap.md) e o
[panorama do mercado](docs/landscape.md).

## Licença

Apache-2.0. Feito pela [Nilum](https://nilum.com.br) e contribuidores.
