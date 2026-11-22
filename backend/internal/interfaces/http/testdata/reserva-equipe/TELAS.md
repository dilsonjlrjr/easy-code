---
id: TELAS
tipo: TELAS
titulo: Mapa de telas — Reserva de salas
autor: designer-agent
onda: DSG-01
versao: 1
estado: rascunho
vinculos: [US-01]
historico:
  - {versao: 1, data: "2026-11-22", onda: DSG-01, descricao: criação}
fluxo: docs/design/fluxo.md
prototipo: docs/design/prototipo/
estilo: docs/design/estilo.md
telas:
  - id: TL-1
    nome: Reservar sala
    arquivo: docs/design/prototipo/reservar.html
    us: [US-01]
    criterios: [US-01/CA-1, US-01/CA-2]
    estados: [vazio, carregando, erro, sucesso, sem_permissao]
acessibilidade:
  nivel: wcag22_aa
  verificado: true
lacunas_contrato: []
---
# Mapa de telas — Reserva de salas

| Tela | US | Critérios | Estados |
|---|---|---|---|
| TL-1 Reservar sala | US-01 | CA-1, CA-2 | vazio, carregando, erro, sucesso, sem_permissao |
