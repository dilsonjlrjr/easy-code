<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import { moneyShare } from '@/shared/format/formatters'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ProjectCosts } from '@/features/projects/api/projectsApi'
import type { DecisionRecord } from '@/features/gates/api/gatesApi'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import { acceptanceExpired, splitOpenFindings, type ReleaseSummary } from '../api/releaseApi'

/**
 * ESP-17 3.4, "Consolidação": every gate with who decided, the tests, security, the open findings
 * (Critical and High as blocking), the accepted risks with their justification and validity
 * (US-33 CA-4, ER-17-6) and the total cost against the budget (ER-17-18).
 */
const props = defineProps<{
  projectId: string
  summary: ReleaseSummary
  costs?: ProjectCosts
}>()

const i18n = useI18n()
const { t } = i18n
const format = useFormat()
const today = new Date().toISOString().slice(0, 10)

const findings = computed(() => splitOpenFindings(props.summary.open_findings))
const security = computed(() => props.summary.gates.find((gate) => gate.gate === 'gate.security'))
const budgetShare = computed(() =>
  moneyShare(props.summary.cost_total.total, props.costs?.budget?.stop_limit),
)

function gateName(gate: string): string {
  const stage = gate.replace(/^gate\./, '')
  return i18n.te(`stages.${stage}`) ? t(`stages.${stage}`) : gate
}

function who(record: DecisionRecord): string {
  if (record.per.type === 'human') return record.per.user?.name ?? record.per.id ?? ''
  return t('agents.orchestrator')
}

function findingLine(finding: FindingSummary): string {
  return `${finding.title} · ${t(`findings.severity.${finding.severity}`)} · ${t(`states.finding.${finding.status}`)}`
}
</script>

<template>
  <!-- Layout: canvas EtapaRelease, "O que a Release consolida" (cards with .tbl and .li rows). -->
  <div class="consolidation">
    <section aria-labelledby="release-gates-heading" class="card rc-card">
      <h3 id="release-gates-heading">{{ $t('release.consolidation.gates') }}</h3>
      <div class="table-scroll">
        <table class="tbl" data-testid="release-gates">
          <caption class="sr-only">
            {{
              $t('release.consolidation.gates')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('release.consolidation.gate') }}</th>
              <th scope="col">{{ $t('release.consolidation.state') }}</th>
              <th scope="col">{{ $t('release.consolidation.decidedBy') }}</th>
              <th scope="col">{{ $t('release.consolidation.when') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="gate in summary.gates" :key="gate.gate">
              <th scope="row" class="row-head">{{ gateName(gate.gate) }}</th>
              <td>
                <StateLabel v-if="gate.state" machine="gate" :state="gate.state" />
                <span v-else class="chip mute">{{ $t('release.consolidation.notEvaluated') }}</span>
              </td>
              <td>{{ gate.decision ? who(gate.decision.record) : '' }}</td>
              <td>{{ gate.decision ? format.dateTime(gate.decision.record.when) : '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section aria-labelledby="release-tests-heading" class="card rc-card">
      <h3 id="release-tests-heading">{{ $t('release.consolidation.tests') }}</h3>
      <p v-if="summary.tests" class="li" data-testid="release-tests">
        <span class="rc-text">
          {{
            $t('release.consolidation.testCounts', {
              total: format.integer(summary.tests.ct_total ?? 0),
              passed: format.integer(summary.tests.passed ?? 0),
              failed: format.integer(summary.tests.failed ?? 0),
            })
          }}
        </span>
        <template v-if="summary.tests.report">
          <span class="sr-only">·</span>
          <router-link
            :to="{
              name: ROUTE_NAMES.artifact,
              params: { projectId, artifactId: summary.tests.report },
            }"
            class="chip mute mono"
            >{{ summary.tests.report }}</router-link
          >
        </template>
      </p>
      <p v-else class="li t2">{{ $t('release.consolidation.noTests') }}</p>
      <p class="li" data-testid="release-security">
        <span class="rc-text">{{ $t('release.consolidation.security') }}:</span>
        <StateLabel v-if="security?.state" machine="gate" :state="security.state" />
        <span v-else class="chip mute">{{ $t('release.consolidation.notEvaluated') }}</span>
      </p>
    </section>

    <section aria-labelledby="release-findings-heading" class="card rc-card">
      <h3 id="release-findings-heading">{{ $t('release.consolidation.openFindings') }}</h3>
      <p v-if="summary.open_findings.length === 0" class="li t2">
        {{ $t('release.consolidation.noOpenFindings') }}
      </p>
      <template v-else>
        <p v-if="findings.blocking.length" class="lb rc-sub">
          {{ $t('release.consolidation.blockingFindings') }}
        </p>
        <ul class="plain-list rc-list" data-testid="release-blocking-findings">
          <li v-for="finding in findings.blocking" :key="finding.id" class="li">
            <router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
              class="chip mute mono"
              >{{ finding.id }}</router-link
            >
            <span class="rc-text">· {{ findingLine(finding) }}</span>
            <span class="chip crit">{{ $t('projects.stage.blocking') }}</span>
          </li>
        </ul>
        <p v-if="findings.other.length" class="lb rc-sub">
          {{ $t('release.consolidation.nonBlockingFindings') }}
        </p>
        <ul class="plain-list rc-list" data-testid="release-other-findings">
          <li v-for="finding in findings.other" :key="finding.id" class="li">
            <router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
              class="chip mute mono"
              >{{ finding.id }}</router-link
            >
            <span class="rc-text">· {{ findingLine(finding) }}</span>
          </li>
        </ul>
      </template>
    </section>

    <!-- US-33 CA-4, CA-17-3: each accepted risk with who, when, justification and validity. -->
    <section aria-labelledby="release-risks-heading" class="card rc-card">
      <h3 id="release-risks-heading">{{ $t('release.consolidation.acceptedRisks') }}</h3>
      <p v-if="summary.accepted_risks.length === 0" class="li t2">
        {{ $t('release.consolidation.noAcceptedRisks') }}
      </p>
      <ul v-else class="plain-list rc-list" data-testid="release-accepted-risks">
        <li
          v-for="risk in summary.accepted_risks"
          :key="risk.finding.id"
          class="li risk-item"
          :data-testid="`accepted-risk-${risk.finding.id}`"
        >
          <p class="risk-line risk-title">
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: risk.finding.id },
              }"
              class="chip mute mono"
              >{{ risk.finding.id }}</router-link
            >
            <span>
              · {{ risk.finding.title }} · {{ $t(`findings.severity.${risk.finding.severity}`) }} ·
            </span>
            <span
              class="chip"
              :class="
                risk.finding.acceptance_expired || acceptanceExpired(risk.acceptance, today)
                  ? 'crit'
                  : 'warn'
              "
            >
              {{
                risk.finding.acceptance_expired || acceptanceExpired(risk.acceptance, today)
                  ? $t('release.consolidation.acceptedExpired')
                  : $t('release.consolidation.accepted')
              }}
            </span>
          </p>
          <p class="risk-line t2">
            {{
              $t('release.consolidation.acceptedBy', {
                who: risk.acceptance.per.name,
                when: format.dateTime(risk.acceptance.when),
              })
            }}
            ·
            {{
              risk.acceptance.valid_until
                ? $t('release.consolidation.validUntil', {
                    date: format.date(risk.acceptance.valid_until),
                  })
                : $t('release.consolidation.noExpiry')
            }}
          </p>
          <p class="risk-line">
            <span class="t2">{{ $t('release.consolidation.justification') }}:</span>
            <span class="pre-wrap">{{ risk.acceptance.justification }}</span>
          </p>
        </li>
      </ul>
    </section>

    <section aria-labelledby="release-cost-heading" class="card rc-card">
      <h3 id="release-cost-heading">{{ $t('release.consolidation.cost') }}</h3>
      <p class="li" data-testid="release-cost">
        <span class="rc-text">{{ format.money(summary.cost_total.total) }}</span>
        <template v-if="budgetShare !== undefined">
          <span class="t2">
            ·
            {{
              $t('release.consolidation.costShare', {
                percentage: format.percent(budgetShare),
                limit: format.money(costs?.budget?.stop_limit),
              })
            }}
          </span>
        </template>
      </p>
    </section>
  </div>
</template>

<style scoped>
.consolidation {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.rc-card {
  padding: 18px 22px;
}

.rc-card h3 {
  margin: 0 0 4px;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.rc-card p.li {
  margin: 0;
}

.rc-text {
  flex: 1 1 240px;
  min-width: 0;
}

.row-head {
  font-weight: 400;
  color: #f1f3ff;
}

.rc-sub {
  margin: 10px 0 4px;
}

.rc-list {
  margin: 0;
}

.risk-item {
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.risk-line {
  margin: 0;
  font-size: 13px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.risk-title {
  font-size: 14px;
  font-weight: 600;
}

.pre-wrap {
  white-space: pre-wrap;
}
</style>
