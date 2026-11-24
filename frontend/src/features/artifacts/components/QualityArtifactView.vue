<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import { referencedArtifact, type StructuredArtifact } from '../composables/structuredArtifact'

/**
 * Structured content of the QA (CT, REL-QA; US-22) and Security (AMEACAS, REL-SEG; US-23)
 * artifacts, following ESP-15 and ESP-16. Everything here is generated content: text only, never
 * HTML (RNF-05); only ids that match the artifact patterns become links.
 */
const props = defineProps<{ projectId: string; artifact: StructuredArtifact }>()
const i18n = useI18n()
const { t } = i18n
const format = useFormat()

const ct = computed(() => (props.artifact.type === 'CT' ? props.artifact.header : undefined))
const relQa = computed(() => (props.artifact.type === 'REL-QA' ? props.artifact.header : undefined))
const ameacas = computed(() =>
  props.artifact.type === 'AMEACAS' ? props.artifact.header : undefined,
)
const relSeg = computed(() =>
  props.artifact.type === 'REL-SEG' ? props.artifact.header : undefined,
)
const covers = computed(() => {
  const value = ct.value?.cobre
  return Array.isArray(value) ? value : value ? [value] : []
})
const SEVERITY_ROWS = ['critico', 'alto', 'medio', 'baixo'] as const

const FINDING_ID = /^ACH-[0-9]{2,}$/
const WAVE_ID = /^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$/

function label(prefix: string, value: string): string {
  const key = `${prefix}.${value}`
  return i18n.te(key) ? t(key) : value
}

function referenceRoute(reference: string): RouteLocationRaw | undefined {
  if (FINDING_ID.test(reference))
    return {
      name: ROUTE_NAMES.finding,
      params: { projectId: props.projectId, findingId: reference },
    }
  if (WAVE_ID.test(reference))
    return { name: ROUTE_NAMES.wave, params: { projectId: props.projectId, waveId: reference } }
  const artifactId = referencedArtifact(reference)
  return artifactId
    ? { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
    : undefined
}
</script>

<template>
  <!-- Look: canvas "Artefato" side cards (h2, `.kv` rows, `.tbl` tables). -->
  <section aria-labelledby="quality-heading" class="card sv" data-testid="structured-view">
    <h2 id="quality-heading">
      {{ $t(`structured.title.${props.artifact.type}`) }}
    </h2>

    <!-- CT (ESP-15 3.3): what it covers, steps and the last execution. -->
    <template v-if="ct">
      <dl class="sv-fields">
        <dt>{{ $t('structured.ct.covers') }}</dt>
        <dd>
          <template v-for="(item, index) in covers" :key="item">
            <router-link v-if="referenceRoute(item)" :to="referenceRoute(item)!" class="mono">{{
              item
            }}</router-link><span v-else class="mono">{{ item }}</span
            ><span v-if="index < covers.length - 1">, </span>
          </template>
        </dd>
        <dt>{{ $t('structured.ct.level') }}</dt>
        <dd>{{ label('structured.levels', ct.nivel) }}</dd>
        <dt>{{ $t('structured.ct.caseType') }}</dt>
        <dd>{{ label('structured.caseTypes', ct.tipo_caso) }}</dd>
        <template v-if="ct.execucao">
          <dt>{{ $t('structured.ct.result') }}</dt>
          <dd data-testid="ct-result">
            {{ label('structured.results', ct.execucao.resultado) }}
            <template v-if="ct.execucao.achado">
              ·
              <router-link
                v-if="referenceRoute(ct.execucao.achado)"
                :to="referenceRoute(ct.execucao.achado)!"
                class="mono"
                >{{ ct.execucao.achado }}</router-link
              >
            </template>
          </dd>
          <template v-if="ct.execucao.teste">
            <dt>{{ $t('structured.ct.test') }}</dt>
            <dd class="mono">{{ ct.execucao.teste }}</dd>
          </template>
          <template v-if="ct.execucao.onda">
            <dt>{{ $t('artifacts.wave') }}</dt>
            <dd>
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId, waveId: ct.execucao.onda },
                  hash: '#evidence',
                }"
                class="mono"
                >{{ ct.execucao.onda }}</router-link
              >
            </dd>
          </template>
          <template v-if="ct.execucao.evidencia">
            <dt>{{ $t('gates.evidence') }}</dt>
            <dd class="pre-wrap">{{ ct.execucao.evidencia }}</dd>
          </template>
        </template>
      </dl>
      <h3>{{ $t('structured.ct.preconditions') }}</h3>
      <ul class="sv-rows">
        <li v-for="(item, index) in ct.precondicoes" :key="index">
          {{ item }}
        </li>
      </ul>
      <h3>{{ $t('structured.ct.steps') }}</h3>
      <ol class="sv-steps">
        <li v-for="(item, index) in ct.passos" :key="index">{{ item }}</li>
      </ol>
      <h3>{{ $t('structured.ct.expected') }}</h3>
      <ul class="sv-rows">
        <li v-for="(item, index) in ct.resultado_esperado" :key="index">
          {{ item }}
        </li>
      </ul>
    </template>

    <!-- REL-QA (ESP-15 3.3): totals, gaps first, the US × CA × CT matrix and the verdict. -->
    <template v-if="relQa">
      <p class="sv-text" data-testid="rel-qa-verdict">
        {{ $t('structured.verdict', { verdict: label('structured.verdicts', relQa.veredito) }) }}
      </p>
      <p class="sv-text" data-testid="rel-qa-summary">
        {{
          $t('structured.relQa.summary', {
            executed: relQa.resumo.ct_executados,
            passed: relQa.resumo.passaram,
            failed: relQa.resumo.falharam,
            blocked: relQa.resumo.bloqueados,
            covered: relQa.resumo.ca_must_cobertos,
            total: relQa.resumo.ca_must_total,
          })
        }}
      </p>
      <div
        v-if="relQa.sem_cobertura.length > 0"
        class="note note-warn"
        role="alert"
        data-testid="rel-qa-gaps"
      >
        <p class="sv-text sv-strong">{{ $t('structured.relQa.gaps') }}</p>
        <ul class="sv-rows">
          <li v-for="gap in relQa.sem_cobertura" :key="`${gap.us}-${gap.criterio}`">
            <span class="mono">{{ gap.us }}/{{ gap.criterio }}</span> · {{ gap.motivo }}
          </li>
        </ul>
      </div>
      <div class="table-scroll">
        <table class="tbl" data-testid="rel-qa-matrix">
          <caption class="sr-only">
            {{
              $t('structured.relQa.matrix')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('traceability.coverage.us') }}</th>
              <th scope="col">{{ $t('structured.relQa.criterion') }}</th>
              <th scope="col">{{ $t('structured.relQa.scenario') }}</th>
              <th scope="col">{{ $t('structured.ct.level') }}</th>
              <th scope="col">{{ $t('structured.ct.result') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in relQa.matriz" :key="`${row.us}-${row.criterio}-${row.ct}`">
              <td>
                <router-link
                  v-if="referenceRoute(row.us)"
                  :to="referenceRoute(row.us)!"
                  class="mono"
                  >{{ row.us }}</router-link
                >
                <span v-else class="mono">{{ row.us }}</span>
              </td>
              <td class="mono">{{ row.criterio }}</td>
              <td>
                <router-link
                  v-if="referenceRoute(row.ct)"
                  :to="referenceRoute(row.ct)!"
                  class="mono"
                  >{{ row.ct }}</router-link
                >
                <span v-else class="mono">{{ row.ct }}</span>
              </td>
              <td>{{ label('structured.levels', row.nivel) }}</td>
              <td>
                {{ label('structured.results', row.resultado) }}
                <template v-if="row.achado">
                  ·
                  <router-link :to="referenceRoute(row.achado) ?? ''" class="mono">{{
                    row.achado
                  }}</router-link>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="relQa.achados_abertos.length" class="sv-text">
        {{ $t('structured.openFindings') }}
        <template v-for="(id, index) in relQa.achados_abertos" :key="id">
          <router-link :to="referenceRoute(id) ?? ''" class="mono">{{ id }}</router-link><span
            v-if="index < relQa.achados_abertos.length - 1"
            >,
          </span>
        </template>
      </p>
    </template>

    <!-- AMEACAS (ESP-16 3.7): threats with STRIDE, severity, mitigation and status. -->
    <template v-if="ameacas">
      <p class="sv-text">{{ ameacas.fluxo }}</p>
      <div class="table-scroll">
        <table class="tbl" data-testid="threats-table">
          <caption class="sr-only">
            {{
              $t('structured.title.AMEACAS')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('structured.threats.id') }}</th>
              <th scope="col">{{ $t('findings.columns.severity') }}</th>
              <th scope="col">{{ $t('structured.threats.stride') }}</th>
              <th scope="col">{{ $t('structured.threats.description') }}</th>
              <th scope="col">{{ $t('structured.threats.mitigation') }}</th>
              <th scope="col">{{ $t('findings.columns.status') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="threat in ameacas.ameacas" :key="threat.id">
              <th scope="row" class="mono">{{ threat.id }}</th>
              <td>{{ label('structured.severities', threat.severidade) }}</td>
              <td>{{ threat.stride.join(', ') }}</td>
              <td>{{ threat.descricao }}</td>
              <td>{{ threat.mitigacao ?? '' }}</td>
              <td>
                {{ label('structured.threatStatus', threat.status) }}
                <template v-if="threat.achado">
                  ·
                  <router-link :to="referenceRoute(threat.achado) ?? ''" class="mono">{{
                    threat.achado
                  }}</router-link>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- REL-SEG (ESP-16 3.4, 3.6, 3.8): verdict, summary, scans, OWASP and false positives. -->
    <template v-if="relSeg">
      <p class="sv-text" data-testid="rel-seg-verdict">
        {{ $t('structured.verdict', { verdict: label('structured.verdicts', relSeg.veredito) }) }}
      </p>
      <div class="table-scroll">
        <table class="tbl" data-testid="rel-seg-summary">
          <caption class="sr-only">
            {{
              $t('structured.relSeg.summary')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('findings.columns.severity') }}</th>
              <th scope="col">{{ $t('structured.relSeg.open') }}</th>
              <th scope="col">{{ $t('structured.relSeg.closed') }}</th>
              <th scope="col">{{ $t('structured.relSeg.accepted') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="severity in SEVERITY_ROWS" :key="severity">
              <th scope="row">
                {{ label('structured.severities', severity) }}
              </th>
              <td>{{ relSeg.resumo[severity].abertos }}</td>
              <td>{{ relSeg.resumo[severity].fechados }}</td>
              <td>{{ relSeg.resumo[severity].aceitos }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <h3>{{ $t('structured.relSeg.scans') }}</h3>
      <div class="table-scroll">
        <table class="tbl" data-testid="rel-seg-scans">
          <caption class="sr-only">
            {{
              $t('structured.relSeg.scans')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('structured.relSeg.tool') }}</th>
              <th scope="col">{{ $t('structured.relSeg.command') }}</th>
              <th scope="col">{{ $t('structured.relSeg.real') }}</th>
              <th scope="col">{{ $t('structured.relSeg.falsePositives') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="scan in relSeg.varreduras" :key="`${scan.ferramenta}-${scan.comando}`">
              <th scope="row">{{ scan.ferramenta }} {{ scan.versao }}</th>
              <td class="mono">{{ scan.comando }}</td>
              <td>
                <template v-if="scan.achados.length === 0">{{ format.integer(0) }}</template>
                <template v-for="(id, index) in scan.achados" :key="id">
                  <router-link :to="referenceRoute(id) ?? ''" class="mono">{{
                    id
                  }}</router-link><span v-if="index < scan.achados.length - 1">, </span>
                </template>
              </td>
              <td>{{ scan.falsos_positivos }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <h3>{{ $t('structured.relSeg.owasp') }}</h3>
      <ul class="sv-rows" data-testid="rel-seg-owasp">
        <li v-for="item in relSeg.owasp" :key="item.categoria">
          <span class="mono">{{ item.categoria }}</span> ·
          {{ label('structured.owasp', item.categoria) }} · {{ item.resultado }}
          <template v-for="id in item.achados" :key="id">
            ·
            <router-link :to="referenceRoute(id) ?? ''" class="mono">{{ id }}</router-link>
          </template>
        </li>
      </ul>
      <template v-if="relSeg.falsos_positivos.length">
        <h3>
          {{ $t('structured.relSeg.falsePositives') }}
        </h3>
        <ul class="sv-rows">
          <li v-for="(item, index) in relSeg.falsos_positivos" :key="index">
            {{ item.ferramenta }} · <span class="mono">{{ item.regra }}</span> ·
            <span class="mono">{{ item.local }}</span> · {{ item.justificativa }}
          </li>
        </ul>
      </template>
    </template>
  </section>
</template>

<style scoped>
.sv {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.sv h2 {
  margin-bottom: 2px;
}

.sv h3 {
  margin: 8px 0 0;
  font-size: 14px;
  line-height: 20px;
  font-weight: 700;
}

.sv-text {
  margin: 0;
  font-size: 13px;
}

.sv-strong {
  font-weight: 700;
}

.sv-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

/* Label / value rows (canvas `.kv` separators). */
.sv-fields {
  display: grid;
  grid-template-columns: minmax(140px, max-content) 1fr;
  column-gap: 12px;
  margin: 0;
}

.sv-fields > dt,
.sv-fields > dd {
  margin: 0;
  padding: 10px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  min-width: 0;
  overflow-wrap: anywhere;
}

.sv-fields > dt {
  color: #a9b0d6;
}

/* Lists of generated items: one per row, canvas `.li` separators. */
.sv-rows {
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: 13px;
}

.sv-rows > li {
  padding: 8px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  overflow-wrap: anywhere;
}

.sv-steps {
  margin: 0;
  padding-left: 22px;
  font-size: 13px;
}

.sv-steps > li + li {
  margin-top: 4px;
}

.tbl th[scope='row'] {
  font-weight: 400;
  color: #f1f3ff;
}

.pre-wrap {
  white-space: pre-wrap;
}

.note-warn {
  flex-direction: column;
  gap: 6px;
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}
</style>
