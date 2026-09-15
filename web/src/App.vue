<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { calculate, compare } from './api'
import {
  ApiError,
  FREQUENCIES,
  type CalcResponse,
  type CandidateLabel,
  type CompareResponse,
  type FieldError
} from './types'
import { formatTenths, parseNumber } from './forms'

type Mode = 'single' | 'compare'

// 默认入口仍是既有单耳罩核算；可切换到双候选对比。
const mode = ref<Mode>('single')

// 六个现场声级由两种模式共享：从核算页切到对比页时直接复用同一组 L。
const levels = reactive<string[]>(FREQUENCIES.map(() => ''))
// 单耳罩模式的逐带衰减。
const attenuations = reactive<string[]>(FREQUENCIES.map(() => ''))
// 对比模式下甲、乙两款候选的逐带衰减。
const attByCandidate = reactive<Record<CandidateLabel, string[]>>({
  甲: FREQUENCIES.map(() => ''),
  乙: FREQUENCIES.map(() => '')
})

const result = ref<CalcResponse | null>(null)
const compareResult = ref<CompareResponse | null>(null)
const submitting = ref(false)
const serverError = ref('')
const serverFields = ref<FieldError[]>([])
const bannerErrors = ref<string[]>([])
const compareServerError = ref('')
const compareFields = ref<FieldError[]>([])
const compareBannerErrors = ref<string[]>([])

// 输入框 DOM 引用，用于 422 后聚焦第一个出错字段。
// 单耳罩键：`${f}:level` / `${f}:attenuation`；
// 对比键：`cmp:L:${f}`（共享声级）、`cmp:${label}:${f}`（候选衰减）。
const inputs = new Map<string, HTMLInputElement>()
function registerInput(key: string) {
  return (el: Element | { $el?: unknown } | null) => {
    if (el instanceof HTMLInputElement) inputs.set(key, el)
    else inputs.delete(key)
  }
}

// ---- 单耳罩模式的服务端错误映射 ----
const fieldErrorSet = computed(() => {
  const s = new Set<string>()
  for (const e of serverFields.value) {
    if (e.field === 'level' || e.field === 'attenuation') s.add(`${e.frequency}:${e.field}`)
  }
  return s
})

const fieldMessages = computed(() => {
  const m = new Map<string, string>()
  for (const e of serverFields.value) {
    if (e.field === 'level' || e.field === 'attenuation') m.set(`${e.frequency}:${e.field}`, e.message)
  }
  return m
})

// ---- 对比模式的服务端错误映射（含候选标识维度）----
function compareLevelKey(f: number) {
  return `cmp:L:${f}`
}
function compareAttKey(label: CandidateLabel, f: number) {
  return `cmp:${label}:${f}`
}

const compareFieldErrorSet = computed(() => {
  const s = new Set<string>()
  for (const e of compareFields.value) {
    if (e.field === 'level' && !e.candidate) s.add(compareLevelKey(e.frequency ?? 0))
    if (e.field === 'attenuation' && e.candidate) s.add(compareAttKey(e.candidate, e.frequency ?? 0))
  }
  return s
})

const compareFieldMessages = computed(() => {
  const m = new Map<string, string>()
  for (const e of compareFields.value) {
    if (e.field === 'level' && !e.candidate) m.set(compareLevelKey(e.frequency ?? 0), e.message)
    if (e.field === 'attenuation' && e.candidate) m.set(compareAttKey(e.candidate, e.frequency ?? 0), e.message)
  }
  return m
})

// 提交时任何错误都会清除旧结果，绝不展示上一次或部分计算。
function clearResultAndErrors() {
  result.value = null
  serverFields.value = []
  bannerErrors.value = []
  serverError.value = ''
}

function clearCompareResultAndErrors() {
  compareResult.value = null
  compareFields.value = []
  compareBannerErrors.value = []
  compareServerError.value = ''
}

// 切换模式不改动已录入数据，但两种模式的旧结果与错误一律清除：
// 隐藏的结果不再对应当前可见表单，回来时必须重新提交。
function switchMode(next: Mode) {
  if (mode.value === next) return
  mode.value = next
  clearResultAndErrors()
  clearCompareResultAndErrors()
}

function firstInvalidKey(): string | null {
  for (const f of FREQUENCIES) {
    if (fieldErrorSet.value.has(`${f}:level`)) return `${f}:level`
    if (fieldErrorSet.value.has(`${f}:attenuation`)) return `${f}:attenuation`
  }
  return null
}

// 对比模式按页面从上到下、从甲到乙的顺序取第一个出错字段。
function firstCompareInvalidKey(): string | null {
  for (const f of FREQUENCIES) {
    if (compareFieldErrorSet.value.has(compareLevelKey(f))) return compareLevelKey(f)
    if (compareFieldErrorSet.value.has(compareAttKey('甲', f))) return compareAttKey('甲', f)
    if (compareFieldErrorSet.value.has(compareAttKey('乙', f))) return compareAttKey('乙', f)
  }
  return null
}

function focusKey(key: string | null) {
  if (!key) return
  inputs.get(key)?.focus()
  inputs.get(key)?.select()
}

// 用户修改任一输入时，清除该字段的服务端错误与旧结果：
// 一旦数据被改动，旧结果即不再对应当前表单。
function onInput(key: string) {
  const [fs, field] = key.split(':')
  const f = Number(fs)
  serverFields.value = serverFields.value.filter((e) => !(e.frequency === f && e.field === field))
  if (result.value) result.value = null
}

// 对比模式：编辑任一输入（共享声级或任一候选衰减）都使整份对比结果失效。
function onCompareLevelInput(f: number) {
  compareFields.value = compareFields.value.filter(
    (e) => !(e.frequency === f && e.field === 'level' && !e.candidate)
  )
  compareResult.value = null
}

function onCompareAttInput(label: CandidateLabel, f: number) {
  compareFields.value = compareFields.value.filter(
    (e) => !(e.frequency === f && e.field === 'attenuation' && e.candidate === label)
  )
  compareResult.value = null
}

async function onSubmit() {
  clearResultAndErrors()

  // 空值以 null 提交；是否合法一律由 Go API 整单裁决，前端不做提交拦截。
  const payload = {
    bands: FREQUENCIES.map((f, i) => ({
      frequency: f,
      level: parseNumber(levels[i]),
      attenuation: parseNumber(attenuations[i])
    }))
  }

  submitting.value = true
  try {
    // 所有计算结果均来自 API，前端仅原样展示，保证界面与 API 一致。
    result.value = await calculate(payload)
  } catch (e) {
    result.value = null
    if (e instanceof ApiError) {
      serverError.value = e.message
      serverFields.value = e.fields
      bannerErrors.value = e.fields
        .filter((f) => f.field === 'bands' || f.field === 'frequency')
        .map((f) => f.message)
      focusKey(firstInvalidKey())
    } else {
      serverError.value = `无法连接核算服务：${e instanceof Error ? e.message : String(e)}`
    }
  } finally {
    submitting.value = false
  }
}

async function onCompareSubmit() {
  clearCompareResultAndErrors()

  // 六个现场声级只提交一次，甲、乙各自只带逐带衰减；合法性整单由 Go API 裁决。
  const payload = {
    levels: FREQUENCIES.map((_, i) => parseNumber(levels[i])),
    candidates: (['甲', '乙'] as const).map((label) => ({
      label,
      bands: FREQUENCIES.map((f, i) => ({
        frequency: f,
        attenuation: parseNumber(attByCandidate[label][i])
      }))
    }))
  }

  submitting.value = true
  try {
    // 两份完整结果与“哪款更优/效果相同”的结论全部来自 API，前端不做任何判定。
    compareResult.value = await compare(payload)
  } catch (e) {
    compareResult.value = null
    if (e instanceof ApiError) {
      compareServerError.value = e.message
      compareFields.value = e.fields
      // 声级数错误、候选缺行/标识错误、频带顺序错误等无法落到具体输入框的信息进横幅。
      compareBannerErrors.value = e.fields
        .filter((f) => f.field === 'levels' || f.field === 'candidates' || f.field === 'bands' || f.field === 'frequency')
        .map((f) => f.message)
      focusKey(firstCompareInvalidKey())
    } else {
      compareServerError.value = `无法连接核算服务：${e instanceof Error ? e.message : String(e)}`
    }
  } finally {
    submitting.value = false
  }
}

function resetForm() {
  for (let i = 0; i < FREQUENCIES.length; i++) {
    levels[i] = ''
    attenuations[i] = ''
  }
  clearResultAndErrors()
}

function resetCompareForm() {
  for (let i = 0; i < FREQUENCIES.length; i++) {
    levels[i] = ''
    attByCandidate.甲[i] = ''
    attByCandidate.乙[i] = ''
  }
  clearCompareResultAndErrors()
}

// 结果区逐行 C 值直接使用 API 行数据，避免任何本地重算。
const resultRows = computed(() => result.value?.rows ?? [])

// 对比面板按候选标识取结果，顺序固定为甲、乙。
const comparePanels = computed(() => {
  if (!compareResult.value) return []
  return (['甲', '乙'] as const)
    .map((label) => compareResult.value?.candidates.find((c) => c.label === label))
    .filter((c): c is NonNullable<typeof c> => Boolean(c))
})
</script>

<template>
  <main class="page">
    <header>
      <h1>耳罩佩戴后合成声级核算</h1>
      <p class="hint">
        逐行录入六个倍频带（125–4000 Hz）的现场声级 <strong>L</strong> 与厂家耳罩衰减值
        <strong>A</strong>。系统先计算各行 <strong>C = L − A</strong>，再按
        <strong>E = 10 × log₁₀( Σ 10^(C/10) )</strong> 对数能量合成，<em>不能</em>对各行取算术平均。
      </p>
    </header>

    <!-- 模式切换：单耳罩核算为默认入口，对比模式复用同一组六个现场声级 -->
    <div class="mode-switch" role="group" aria-label="核算模式">
      <button
        type="button"
        :class="{ active: mode === 'single' }"
        :aria-pressed="mode === 'single'"
        data-testid="mode-single"
        @click="switchMode('single')"
      >
        单耳罩核算
      </button>
      <button
        type="button"
        :class="{ active: mode === 'compare' }"
        :aria-pressed="mode === 'compare'"
        data-testid="mode-compare"
        @click="switchMode('compare')"
      >
        双候选对比
      </button>
    </div>

    <!-- ============ 既有单耳罩表单（默认） ============ -->
    <form v-if="mode === 'single'" novalidate @submit.prevent="onSubmit">
      <table class="bands">
        <thead>
          <tr>
            <th>倍频带中心频率 (Hz)</th>
            <th>现场声级 L (dB)</th>
            <th>耳罩衰减 A (dB)</th>
            <th>佩戴后 C = L − A (dB)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(f, i) in FREQUENCIES" :key="f">
            <td class="freq">{{ f }}</td>
            <td>
              <input
                :ref="registerInput(`${f}:level`)"
                v-model="levels[i]"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="40"
                max="140"
                :aria-invalid="fieldErrorSet.has(`${f}:level`)"
                aria-label="现场声级 L"
                @input="onInput(`${f}:level`)"
              />
              <p
                v-if="fieldMessages.has(`${f}:level`)"
                class="field-error"
                role="alert"
              >
                {{ fieldMessages.get(`${f}:level`) }}
              </p>
            </td>
            <td>
              <input
                :ref="registerInput(`${f}:attenuation`)"
                v-model="attenuations[i]"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="0"
                max="40"
                :aria-invalid="fieldErrorSet.has(`${f}:attenuation`)"
                aria-label="耳罩衰减 A"
                @input="onInput(`${f}:attenuation`)"
              />
              <p
                v-if="fieldMessages.has(`${f}:attenuation`)"
                class="field-error"
                role="alert"
              >
                {{ fieldMessages.get(`${f}:attenuation`) }}
              </p>
            </td>
            <td class="corrected">
              <template v-if="result">
                {{ formatTenths(resultRows.find((r) => r.frequency === f)?.corrected ?? NaN) }}
              </template>
              <span v-else class="placeholder">—</span>
            </td>
          </tr>
        </tbody>
      </table>

      <p class="range-hint">取值范围：L 为 40.0–140.0 dB，A 为 0.0–40.0 dB，均只允许一位小数。</p>

      <div v-if="serverError" class="banner error" role="alert">
        <strong>{{ serverError }}</strong>
        <ul v-if="bannerErrors.length">
          <li v-for="(msg, i) in bannerErrors" :key="i">{{ msg }}</li>
        </ul>
      </div>

      <div class="actions">
        <button type="submit" :disabled="submitting">
          {{ submitting ? '核算中…' : '计算佩戴后合成声级' }}
        </button>
        <button type="button" class="secondary" @click="resetForm">清空</button>
      </div>
    </form>

    <!-- ============ 双候选对比表单 ============ -->
    <form v-else novalidate @submit.prevent="onCompareSubmit">
      <table class="bands compare-bands">
        <thead>
          <tr>
            <th>倍频带中心频率 (Hz)</th>
            <th>现场声级 L (dB)<span class="th-note">（两款共用）</span></th>
            <th>候选甲衰减 A甲 (dB)</th>
            <th>候选乙衰减 A乙 (dB)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(f, i) in FREQUENCIES" :key="f">
            <td class="freq">{{ f }}</td>
            <td>
              <input
                :ref="registerInput(compareLevelKey(f))"
                v-model="levels[i]"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="40"
                max="140"
                :aria-invalid="compareFieldErrorSet.has(compareLevelKey(f))"
                aria-label="现场声级 L"
                @input="onCompareLevelInput(f)"
              />
              <p
                v-if="compareFieldMessages.has(compareLevelKey(f))"
                class="field-error"
                role="alert"
              >
                {{ compareFieldMessages.get(compareLevelKey(f)) }}
              </p>
            </td>
            <td>
              <input
                :ref="registerInput(compareAttKey('甲', f))"
                v-model="attByCandidate.甲[i]"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="0"
                max="40"
                :aria-invalid="compareFieldErrorSet.has(compareAttKey('甲', f))"
                aria-label="候选耳罩甲衰减 A"
                @input="onCompareAttInput('甲', f)"
              />
              <p
                v-if="compareFieldMessages.has(compareAttKey('甲', f))"
                class="field-error"
                role="alert"
              >
                {{ compareFieldMessages.get(compareAttKey('甲', f)) }}
              </p>
            </td>
            <td>
              <input
                :ref="registerInput(compareAttKey('乙', f))"
                v-model="attByCandidate.乙[i]"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="0"
                max="40"
                :aria-invalid="compareFieldErrorSet.has(compareAttKey('乙', f))"
                aria-label="候选耳罩乙衰减 A"
                @input="onCompareAttInput('乙', f)"
              />
              <p
                v-if="compareFieldMessages.has(compareAttKey('乙', f))"
                class="field-error"
                role="alert"
              >
                {{ compareFieldMessages.get(compareAttKey('乙', f)) }}
              </p>
            </td>
          </tr>
        </tbody>
      </table>

      <p class="range-hint">
        取值范围：L 为 40.0–140.0 dB，A甲、A乙 为 0.0–40.0 dB，均只允许一位小数；一次提交同时核算两款候选。
      </p>

      <div v-if="compareServerError" class="banner error" role="alert">
        <strong>{{ compareServerError }}</strong>
        <ul v-if="compareBannerErrors.length">
          <li v-for="(msg, i) in compareBannerErrors" :key="i">{{ msg }}</li>
        </ul>
      </div>

      <div class="actions">
        <button type="submit" :disabled="submitting">
          {{ submitting ? '对比核算中…' : '同时核算甲、乙耳罩' }}
        </button>
        <button type="button" class="secondary" @click="resetCompareForm">清空</button>
      </div>
    </form>

    <!-- ============ 单耳罩结果 ============ -->
    <section v-if="mode === 'single' && result" class="result" data-testid="result" aria-live="polite">
      <h2>核算结果</h2>

      <table class="result-table">
        <thead>
          <tr>
            <th>频率 (Hz)</th>
            <th>现场 L (dB)</th>
            <th>衰减 A (dB)</th>
            <th>佩戴后 C (dB)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in result.rows" :key="r.frequency">
            <td>{{ r.frequency }}</td>
            <td>{{ formatTenths(r.level) }}</td>
            <td>{{ formatTenths(r.attenuation) }}</td>
            <td data-testid="corrected-cell">{{ formatTenths(r.corrected) }}</td>
          </tr>
        </tbody>
      </table>

      <div class="formula-block">
        <h3>公式</h3>
        <p class="formula" data-testid="formula">{{ result.formula }}</p>
        <h3>公式代入依据</h3>
        <p class="formula substitution" data-testid="substitution">{{ result.substitution }}</p>
      </div>

      <div class="levels">
        <div class="level-card">
          <span class="label">内部未舍入值</span>
          <span class="value exact" data-testid="exact-level">{{ result.exactLevel }} dB</span>
        </div>
        <div class="level-card primary">
          <span class="label">佩戴后合成声级（四舍五入保留一位小数）</span>
          <span class="value display" data-testid="display-level">{{ result.displayLevel }} dB</span>
        </div>
      </div>
    </section>

    <!-- ============ 双候选对比结果 ============ -->
    <section
      v-if="mode === 'compare' && compareResult"
      class="result compare-result"
      data-testid="compare-result"
      aria-live="polite"
    >
      <!-- 结论只原样渲染服务端文案，前端不根据数值自行拼装 -->
      <p class="compare-conclusion" data-testid="compare-conclusion" role="status">
        {{ compareResult.conclusion }}
      </p>

      <div class="compare-panels">
        <div
          v-for="cand in comparePanels"
          :key="cand.label"
          class="compare-panel"
          :class="{ winner: !compareResult.tie && compareResult.winner === cand.label }"
          :data-candidate="cand.label"
          data-testid="compare-candidate"
        >
          <h2>候选耳罩{{ cand.label }}</h2>

          <table class="result-table">
            <thead>
              <tr>
                <th>频率 (Hz)</th>
                <th>现场 L (dB)</th>
                <th>衰减 A (dB)</th>
                <th>佩戴后 C (dB)</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in cand.rows" :key="r.frequency">
                <td>{{ r.frequency }}</td>
                <td>{{ formatTenths(r.level) }}</td>
                <td>{{ formatTenths(r.attenuation) }}</td>
                <td data-testid="compare-corrected-cell">{{ formatTenths(r.corrected) }}</td>
              </tr>
            </tbody>
          </table>

          <div class="formula-block">
            <h3>公式代入依据</h3>
            <p class="formula substitution" data-testid="compare-substitution">{{ cand.substitution }}</p>
          </div>

          <div class="levels">
            <div class="level-card">
              <span class="label">内部未舍入值</span>
              <span class="value exact" data-testid="compare-exact-level">{{ cand.exactLevel }} dB</span>
            </div>
            <div class="level-card primary">
              <span class="label">佩戴后合成声级（四舍五入保留一位小数）</span>
              <span class="value display" data-testid="compare-display-level">{{ cand.displayLevel }} dB</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  </main>
</template>

<style scoped>
* {
  box-sizing: border-box;
}
.page {
  max-width: 1100px;
  margin: 0 auto;
  padding: 24px 20px 48px;
  font-family: 'PingFang SC', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, sans-serif;
  color: #1f2933;
}
h1 {
  font-size: 24px;
  margin: 0 0 8px;
}
.hint {
  color: #52606d;
  line-height: 1.7;
  margin: 0 0 20px;
}
.mode-switch {
  display: inline-flex;
  gap: 0;
  margin-bottom: 16px;
  border: 1px solid #1a7f5a;
  border-radius: 8px;
  overflow: hidden;
}
.mode-switch button {
  background: #fff;
  color: #1a7f5a;
  border: none;
  border-radius: 0;
  padding: 10px 22px;
  font-size: 14px;
}
.mode-switch button + button {
  border-left: 1px solid #1a7f5a;
}
.mode-switch button.active {
  background: #1a7f5a;
  color: #fff;
}
.mode-switch button:hover:not(.active) {
  background: #eaf6f0;
}
form {
  background: #fff;
  border: 1px solid #d9e2ec;
  border-radius: 10px;
  padding: 20px;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  padding: 10px 8px;
  text-align: left;
  border-bottom: 1px solid #e4e7eb;
  vertical-align: top;
}
th {
  font-size: 13px;
  color: #52606d;
  font-weight: 600;
}
.th-note {
  font-weight: 400;
  color: #7b8794;
}
.freq {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
input[type='number'] {
  width: 130px;
  padding: 8px 10px;
  border: 1px solid #9fb3c8;
  border-radius: 6px;
  font-size: 15px;
}
input[type='number']:focus {
  outline: 2px solid #2bb673;
  outline-offset: 1px;
  border-color: #2bb673;
}
input[aria-invalid='true'] {
  border-color: #d64545;
  background: #fff5f5;
}
.field-error {
  color: #d64545;
  font-size: 12px;
  margin: 4px 0 0;
  max-width: 220px;
}
.corrected {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.placeholder {
  color: #9aa5b1;
}
.range-hint {
  color: #7b8794;
  font-size: 12px;
  margin: 12px 0 0;
}
.actions {
  margin-top: 18px;
  display: flex;
  gap: 12px;
}
button {
  padding: 10px 20px;
  border: none;
  border-radius: 6px;
  background: #1a7f5a;
  color: #fff;
  font-size: 15px;
  cursor: pointer;
}
button:hover:not(:disabled) {
  background: #146648;
}
button:disabled {
  opacity: 0.6;
  cursor: wait;
}
button.secondary {
  background: #e4e7eb;
  color: #334e68;
}
button.secondary:hover {
  background: #cbd2d9;
}
.banner {
  margin-top: 16px;
  padding: 12px 16px;
  border-radius: 8px;
}
.banner.error {
  background: #fff5f5;
  border: 1px solid #f0b4b4;
  color: #a61b1b;
}
.banner ul {
  margin: 8px 0 0;
  padding-left: 20px;
}
.result {
  margin-top: 28px;
  background: #f0faf5;
  border: 1px solid #b7e4cf;
  border-radius: 10px;
  padding: 20px;
}
.result h2 {
  margin: 0 0 14px;
  font-size: 19px;
}
.result-table {
  background: #fff;
  border: 1px solid #d9e2ec;
  border-radius: 8px;
  overflow: hidden;
}
.formula-block {
  margin-top: 18px;
}
.formula-block h3 {
  font-size: 14px;
  margin: 12px 0 4px;
  color: #334e68;
}
.formula {
  font-family: ui-monospace, 'Cascadia Mono', Menlo, Consolas, monospace;
  background: #fff;
  border: 1px solid #d9e2ec;
  border-radius: 6px;
  padding: 10px 12px;
  margin: 0;
  overflow-x: auto;
  font-size: 14px;
}
.substitution {
  line-height: 1.8;
}
.levels {
  display: flex;
  gap: 16px;
  margin-top: 20px;
  flex-wrap: wrap;
}
.level-card {
  flex: 1 1 260px;
  background: #fff;
  border: 1px solid #d9e2ec;
  border-radius: 8px;
  padding: 14px 18px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.level-card.primary {
  border-color: #1a7f5a;
  box-shadow: 0 0 0 1px #1a7f5a inset;
}
.level-card .label {
  font-size: 13px;
  color: #52606d;
}
.level-card .value {
  font-size: 26px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.level-card .display {
  color: #1a7f5a;
}
.compare-conclusion {
  margin: 0 0 18px;
  padding: 14px 18px;
  background: #fff;
  border: 1px solid #1a7f5a;
  border-left: 6px solid #1a7f5a;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 600;
  color: #146648;
}
.compare-panels {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.compare-panel {
  background: #f7fbf9;
  border: 1px solid #d9e2ec;
  border-radius: 10px;
  padding: 16px;
  min-width: 0;
}
.compare-panel.winner {
  border-color: #1a7f5a;
  box-shadow: 0 0 0 2px #1a7f54 inset;
}
.compare-panel .formula {
  font-size: 12px;
}
.compare-panel .level-card .value {
  font-size: 20px;
}
@media (max-width: 860px) {
  .compare-panels {
    grid-template-columns: 1fr;
  }
}
</style>
