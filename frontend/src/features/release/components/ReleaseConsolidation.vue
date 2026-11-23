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
  <div>
    <section aria-labelledby="release-gates-heading" class="mb-6">
      <h3 id="release-gates-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('release.consolidation.gates') }}
      </h3>
      <v-table density="compact" data-testid="release-gates">
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
            <th scope="row" class="font-weight-regular">{{ gateName(gate.gate) }}</th>
            <td>
              <StateLabel v-if="gate.state" machine="gate" :state="gate.state" />
              <span v-else>{{ $t('release.consolidation.notEvaluated') }}</span>
            </td>
            <td>{{ gate.decision ? who(gate.decision.record) : '' }}</td>
            <td>{{ gate.decision ? format.dateTime(gate.decision.record.when) : '' }}</td>
          </tr>
        </tbody>
      </v-table>
    </section>

    <section aria-labelledby="release-tests-heading" class="mb-6">
      <h3 id="release-tests-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('release.consolidation.tests') }}
      </h3>
      <p v-if="summary.tests" class="text-body-2" data-testid="release-tests">
        {{
          $t('release.consolidation.testCounts', {
            total: format.integer(summary.tests.ct_total ?? 0),
            passed: format.integer(summary.tests.passed ?? 0),
            failed: format.integer(summary.tests.failed ?? 0),
          })
        }}
        <template v-if="summary.tests.report">
          ·
          <router-link
            :to="{
              name: ROUTE_NAMES.artifact,
              params: { projectId, artifactId: summary.tests.report },
            }"
            class="monospace"
            >{{ summary.tests.report }}</router-link
          >
        </template>
      </p>
      <p v-else class="text-body-2">{{ $t('release.consolidation.noTests') }}</p>
      <p class="text-body-2 mt-1" data-testid="release-security">
        {{ $t('release.consolidation.security') }}:
        {{
          security?.state
            ? $t(`states.gate.${security.state}`)
            : $t('release.consolidation.notEvaluated')
        }}
      </p>
    </section>

    <section aria-labelledby="release-findings-heading" class="mb-6">
      <h3 id="release-findings-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('release.consolidation.openFindings') }}
      </h3>
      <p v-if="summary.open_findings.length === 0" class="text-body-2">
        {{ $t('release.consolidation.noOpenFindings') }}
      </p>
      <template v-else>
        <p v-if="findings.blocking.length" class="text-body-2 font-weight-medium">
          {{ $t('release.consolidation.blockingFindings') }}
        </p>
        <ul class="plain-list mb-2" data-testid="release-blocking-findings">
          <li v-for="finding in findings.blocking" :key="finding.id" class="text-body-2 mb-1">
            <router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
              class="monospace"
              >{{ finding.id }}</router-link
            >
            · {{ findingLine(finding) }} · {{ $t('projects.stage.blocking') }}
          </li>
        </ul>
        <p v-if="findings.other.length" class="text-body-2 font-weight-medium">
          {{ $t('release.consolidation.nonBlockingFindings') }}
        </p>
        <ul class="plain-list" data-testid="release-other-findings">
          <li v-for="finding in findings.other" :key="finding.id" class="text-body-2 mb-1">
            <router-link
              :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
              class="monospace"
              >{{ finding.id }}</router-link
            >
            · {{ findingLine(finding) }}
          </li>
        </ul>
      </template>
    </section>

    <!-- US-33 CA-4, CA-17-3: each accepted risk with who, when, justification and validity. -->
    <section aria-labelledby="release-risks-heading" class="mb-6">
      <h3 id="release-risks-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('release.consolidation.acceptedRisks') }}
      </h3>
      <p v-if="summary.accepted_risks.length === 0" class="text-body-2">
        {{ $t('release.consolidation.noAcceptedRisks') }}
      </p>
      <ul v-else class="plain-list" data-testid="release-accepted-risks">
        <li
          v-for="risk in summary.accepted_risks"
          :key="risk.finding.id"
          class="risk-item mb-3"
          :data-testid="`accepted-risk-${risk.finding.id}`"
        >
          <p class="text-body-2 font-weight-medium">
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: risk.finding.id },
              }"
              class="monospace"
              >{{ risk.finding.id }}</router-link
            >
            · {{ risk.finding.title }} · {{ $t(`findings.severity.${risk.finding.severity}`) }} ·
            {{
              risk.finding.acceptance_expired || acceptanceExpired(risk.acceptance, today)
                ? $t('release.consolidation.acceptedExpired')
                : $t('release.consolidation.accepted')
            }}
          </p>
          <p class="text-body-2">
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
          <p class="text-body-2">
            {{ $t('release.consolidation.justification') }}:
            <span class="pre-wrap">{{ risk.acceptance.justification }}</span>
          </p>
        </li>
      </ul>
    </section>

    <section aria-labelledby="release-cost-heading" class="mb-6">
      <h3 id="release-cost-heading" class="text-subtitle-1 font-weight-medium mb-2">
        {{ $t('release.consolidation.cost') }}
      </h3>
      <p class="text-body-2" data-testid="release-cost">
        {{ format.money(summary.cost_total.total) }}
        <template v-if="budgetShare !== undefined">
          ·
          {{
            $t('release.consolidation.costShare', {
              percentage: format.percent(budgetShare),
              limit: format.money(costs?.budget?.stop_limit),
            })
          }}
        </template>
      </p>
    </section>
  </div>
</template>

<style scoped>
.pre-wrap {
  white-space: pre-wrap;
}

.risk-item {
  border-left: 3px solid rgb(var(--v-theme-on-surface), 0.2);
  padding-left: 0.75rem;
}
</style>
