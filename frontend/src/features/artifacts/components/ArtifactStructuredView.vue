<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import {
  foreignKeys,
  referencedArtifact,
  type StructuredArtifact,
} from '../composables/structuredArtifact'

/**
 * Structured content of the DBA (DIC, PERSIST, US-20), Designer (TELAS, US-21) and technical
 * validation (VALIDACAO, US-20 CA-4) artifacts, following ESP-09, ESP-10 and ESP-13. Everything
 * here is generated content: text only, never HTML (RNF-05, ADR-19).
 */
const props = defineProps<{ projectId: string; artifact: StructuredArtifact }>()
const i18n = useI18n()
const { t } = i18n

const dic = computed(() => (props.artifact.type === 'DIC' ? props.artifact.header : undefined))
const persist = computed(() =>
  props.artifact.type === 'PERSIST' ? props.artifact.header : undefined,
)
const telas = computed(() => (props.artifact.type === 'TELAS' ? props.artifact.header : undefined))
const validacao = computed(() =>
  props.artifact.type === 'VALIDACAO' ? props.artifact.header : undefined,
)
const relationships = computed(() => (dic.value ? foreignKeys(dic.value) : []))
const backupWithoutRequirement = computed(() => {
  const backup = persist.value?.backup
  return backup && 'sem_requisito' in backup ? backup : undefined
})
const backupPlan = computed(() => {
  const backup = persist.value?.backup
  return backup && 'rpo' in backup ? backup : undefined
})

function label(prefix: string, value: string): string {
  const key = `${prefix}.${value}`
  return i18n.te(key) ? t(key) : value
}

function yesNo(value: boolean | undefined): string {
  return value ? t('common.yes') : t('common.no')
}

function artifactRoute(reference: string) {
  const artifactId = referencedArtifact(reference)
  return artifactId
    ? { name: ROUTE_NAMES.artifact, params: { projectId: props.projectId, artifactId } }
    : undefined
}
</script>

<template>
  <section aria-labelledby="structured-heading" class="mb-6" data-testid="structured-view">
    <h2 id="structured-heading" class="text-h6 mb-2">
      {{ $t(`structured.title.${props.artifact.type}`) }}
    </h2>

    <!-- DIC (ESP-09 3): tables, dictionary, relationships and personal data. -->
    <template v-if="dic">
      <p v-if="dic.modelo_dbml" class="text-body-2 mb-3">
        {{ $t('structured.dic.model') }} <span class="monospace">{{ dic.modelo_dbml }}</span>
      </p>
      <section
        v-for="table in dic.tabelas"
        :key="table.nome"
        :aria-labelledby="`table-${table.nome}`"
        class="mb-5"
        :data-testid="`dic-table-${table.nome}`"
      >
        <h3 :id="`table-${table.nome}`" class="text-subtitle-1 font-weight-medium monospace">
          {{ table.nome }}
        </h3>
        <p class="text-body-2">{{ table.descricao }}</p>
        <p v-if="table.origem?.length" class="text-body-2">
          {{ $t('structured.dic.origin') }}
          <template v-for="(origin, index) in table.origem" :key="origin">
            <router-link v-if="artifactRoute(origin)" :to="artifactRoute(origin) ?? ''">{{
              origin
            }}</router-link><span v-else>{{ origin }}</span
            ><span v-if="index < table.origem.length - 1">, </span>
          </template>
        </p>
        <div class="table-scroll">
          <v-table density="compact">
            <caption class="sr-only">
              {{
                $t('structured.dic.columnsOf', { table: table.nome })
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('structured.dic.column') }}</th>
                <th scope="col">{{ $t('structured.dic.type') }}</th>
                <th scope="col">{{ $t('structured.dic.nullable') }}</th>
                <th scope="col">{{ $t('structured.dic.default') }}</th>
                <th scope="col">{{ $t('structured.dic.description') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="column in table.colunas" :key="column.nome">
                <td class="monospace">
                  {{ column.nome }}
                  <span v-if="column.dado_pessoal" class="font-weight-bold">
                    · {{ $t('structured.dic.personal') }}</span
                  >
                </td>
                <td class="monospace">{{ column.tipo }}</td>
                <td>{{ yesNo(column.nulo) }}</td>
                <td class="monospace">{{ column.padrao ?? '' }}</td>
                <td>{{ column.descricao }}</td>
              </tr>
            </tbody>
          </v-table>
        </div>
        <ul v-if="table.constraints.length" class="ml-4 mt-2">
          <li v-for="constraint in table.constraints" :key="constraint.nome" class="text-body-2">
            <span class="monospace">{{ constraint.nome }}</span> ·
            {{ label('structured.dic.constraintTypes', constraint.tipo) }} ·
            <span class="monospace">{{ constraint.definicao }}</span>
            <template v-if="constraint.regra">
              ·
              <router-link
                v-if="artifactRoute(constraint.regra)"
                :to="artifactRoute(constraint.regra) ?? ''"
                >{{ constraint.regra }}</router-link><span v-else>{{ constraint.regra }}</span>
            </template>
          </li>
        </ul>
      </section>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.dic.relationships') }}
      </h3>
      <p v-if="relationships.length === 0" class="text-body-2 mb-4">
        {{ $t('structured.dic.noRelationships') }}
      </p>
      <ul v-else class="ml-4 mb-4">
        <li v-for="item in relationships" :key="item.constraint.nome" class="text-body-2">
          <span class="monospace">{{ item.table }}</span> ·
          <span class="monospace">{{ item.constraint.definicao }}</span>
        </li>
      </ul>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.dic.personalData') }}
      </h3>
      <p v-if="dic.dados_pessoais.length === 0" class="text-body-2">
        {{ $t('structured.dic.noPersonalData') }}
      </p>
      <div v-else class="table-scroll" data-testid="dic-personal-data">
        <v-table density="compact">
          <thead>
            <tr>
              <th scope="col">{{ $t('structured.dic.field') }}</th>
              <th scope="col">{{ $t('structured.dic.purpose') }}</th>
              <th scope="col">{{ $t('structured.dic.legalBasis') }}</th>
              <th scope="col">{{ $t('structured.dic.retention') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in dic.dados_pessoais" :key="item.campo">
              <td class="monospace">{{ item.campo }}</td>
              <td>{{ item.finalidade }}</td>
              <td>
                {{ label('structured.dic.legalBases', item.base_legal) }}
                <span v-if="item.base_legal === 'a_confirmar'" class="font-weight-bold">
                  · {{ $t('structured.dic.legalBasisPending') }}
                </span>
              </td>
              <td>{{ item.retencao }}</td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </template>

    <!-- PERSIST (ESP-09 3): one block per schema field. -->
    <template v-if="persist">
      <dl class="summary-list mb-4" data-testid="persist-summary">
        <dt>{{ $t('structured.persist.database') }}</dt>
        <dd>{{ persist.banco.sgbd }} {{ persist.banco.versao }}</dd>
        <dt>{{ $t('structured.persist.access') }}</dt>
        <dd>{{ persist.acesso }}</dd>
        <dt>{{ $t('structured.persist.migrations') }}</dt>
        <dd>{{ persist.migracoes.ferramenta }} · {{ persist.migracoes.convencao }}</dd>
        <dt>{{ $t('structured.persist.backup') }}</dt>
        <dd v-if="backupPlan">
          {{
            $t('structured.persist.backupPlan', {
              rpo: backupPlan.rpo,
              rto: backupPlan.rto,
              strategy: backupPlan.estrategia,
            })
          }}
        </dd>
        <dd v-else-if="backupWithoutRequirement">
          {{
            $t('structured.persist.backupNone', {
              reason: backupWithoutRequirement.justificativa,
            })
          }}
        </dd>
        <template v-if="persist.protecao_dados_pessoais">
          <dt>{{ $t('structured.persist.personalDataProtection') }}</dt>
          <dd>{{ persist.protecao_dados_pessoais }}</dd>
        </template>
      </dl>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.persist.concurrency') }}
      </h3>
      <ul class="ml-4 mb-4">
        <li v-for="(item, index) in persist.concorrencia" :key="index" class="text-body-2">
          {{ item.situacao }} · {{ item.garantia }}
        </li>
      </ul>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.persist.indexes') }}
      </h3>
      <ul class="ml-4 mb-4" data-testid="persist-indexes">
        <li v-for="item in persist.indices" :key="item.nome" class="text-body-2 mb-1">
          <span class="monospace">{{ item.nome }}</span> ·
          <span class="monospace">{{ item.tabela }}({{ item.colunas.join(', ') }})</span> ·
          {{ item.tipo }} · {{ item.justificativa }}
          <template v-if="item.custo_escrita_aceito">
            · {{ $t('structured.persist.writeCostAccepted') }}</template
          >
        </li>
      </ul>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.persist.performance') }}
      </h3>
      <ul class="ml-4 mb-4">
        <li v-for="(item, index) in persist.desempenho" :key="index" class="text-body-2 mb-1">
          {{ item.consulta }} · {{ item.volume }} · {{ item.meta }}
          <template v-if="item.rnf">
            ·
            <router-link v-if="artifactRoute(item.rnf)" :to="artifactRoute(item.rnf) ?? ''">{{
              item.rnf
            }}</router-link><span v-else>{{ item.rnf }}</span>
          </template>
          <span v-if="!item.plano_conferido" class="font-weight-bold">
            · {{ $t('structured.persist.planNotChecked') }}</span
          >
        </li>
      </ul>

      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.persist.permissions') }}
      </h3>
      <ul class="ml-4">
        <li v-for="item in persist.permissoes" :key="item.papel" class="text-body-2">
          <span class="monospace">{{ item.papel }}</span> · {{ item.privilegios.join(', ') }}
          <span v-if="item.ddl_em_producao" class="font-weight-bold">
            · {{ $t('structured.persist.ddlInProduction') }}</span
          >
        </li>
      </ul>
    </template>

    <!-- TELAS (ESP-10): the screen ↔ story map, prototype and accessibility. -->
    <template v-if="telas">
      <dl class="summary-list mb-4">
        <dt>{{ $t('structured.telas.flow') }}</dt>
        <dd class="monospace">{{ telas.fluxo }}</dd>
        <dt>{{ $t('structured.telas.prototype') }}</dt>
        <dd class="monospace">{{ telas.prototipo }}</dd>
        <dt>{{ $t('structured.telas.style') }}</dt>
        <dd class="monospace">{{ telas.estilo }}</dd>
        <dt>{{ $t('structured.telas.accessibility') }}</dt>
        <dd>
          {{ label('structured.telas.levels', telas.acessibilidade.nivel) }} ·
          {{
            telas.acessibilidade.verificado
              ? $t('structured.telas.verified')
              : $t('structured.telas.notVerified')
          }}
          <template v-if="telas.acessibilidade.observacoes">
            · {{ telas.acessibilidade.observacoes }}</template
          >
        </dd>
      </dl>
      <div class="table-scroll" data-testid="telas-map">
        <v-table density="compact">
          <caption class="sr-only">
            {{
              $t('structured.telas.map')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('structured.telas.screen') }}</th>
              <th scope="col">{{ $t('structured.telas.stories') }}</th>
              <th scope="col">{{ $t('structured.telas.criteria') }}</th>
              <th scope="col">{{ $t('structured.telas.states') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="screen in telas.telas" :key="screen.id" :data-testid="`screen-${screen.id}`">
              <td>
                <span class="monospace">{{ screen.id }}</span> · {{ screen.nome }}
                <div class="text-body-2 monospace">{{ screen.arquivo }}</div>
              </td>
              <td>
                <template v-for="(story, index) in screen.us" :key="story">
                  <router-link v-if="artifactRoute(story)" :to="artifactRoute(story) ?? ''">{{
                    story
                  }}</router-link><span v-else>{{ story }}</span
                  ><span v-if="index < screen.us.length - 1">, </span>
                </template>
              </td>
              <td class="monospace">{{ screen.criterios.join(', ') }}</td>
              <td>{{ screen.estados.join(', ') }}</td>
            </tr>
          </tbody>
        </v-table>
      </div>
      <template v-if="telas.lacunas_contrato.length">
        <h3 class="text-subtitle-1 font-weight-medium mt-4 mb-1">
          {{ $t('structured.telas.contractGaps') }}
        </h3>
        <ul class="ml-4">
          <li v-for="(gap, index) in telas.lacunas_contrato" :key="index" class="text-body-2">
            {{ gap }}
          </li>
        </ul>
      </template>
    </template>

    <!-- VALIDACAO (ESP-13, US-20 CA-4): cross checks, opinions and the verdict. -->
    <template v-if="validacao">
      <p class="text-body-1 font-weight-medium mb-3" data-testid="validation-verdict">
        {{
          $t('structured.validacao.verdict', {
            verdict: label('structured.validacao.verdicts', validacao.veredito),
          })
        }}
      </p>
      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.validacao.checks') }}
      </h3>
      <ul class="ml-4 mb-4">
        <li v-for="item in validacao.conferencias" :key="item.par" class="text-body-2 mb-1">
          {{ label('structured.validacao.pairs', item.par) }} ·
          {{ label('structured.validacao.results', item.resultado) }}
          <template v-if="item.inconsistencia"> · {{ item.inconsistencia }}</template>
          <template v-if="item.achado">
            · <span class="monospace">{{ item.achado }}</span></template
          >
        </li>
      </ul>
      <h3 class="text-subtitle-1 font-weight-medium mb-1">
        {{ $t('structured.validacao.opinions') }}
      </h3>
      <ul class="ml-4" data-testid="validation-opinions">
        <li v-for="item in validacao.pareceres" :key="item.participante" class="text-body-2 mb-1">
          {{ label('structured.validacao.participants', item.participante) }} ·
          {{ label('structured.validacao.opinionValues', item.parecer) }}
          <template v-if="item.ressalvas.length"> · {{ item.ressalvas.join('; ') }}</template>
        </li>
      </ul>
    </template>
  </section>
</template>
