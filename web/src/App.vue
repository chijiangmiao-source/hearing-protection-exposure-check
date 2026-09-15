<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { calculate } from './api'
import { ApiError, FREQUENCIES, type CalcResponse, type FieldError } from './types'
import { formatTenths, parseNumber, type FormRow } from './forms'

// 六行固定频带，顺序不可增删。
const rows = reactive<FormRow[]>(
  FREQUENCIES.map((f) => ({ frequency: f, level: '', attenuation: '' }))
)

const result = ref<CalcResponse | null>(null)
const submitting = ref(false)
const serverError = ref('')
const serverFields = ref<FieldError[]>([])
const bannerErrors = ref<string[]>([])

// 输入框 DOM 引用，用于 422 后聚焦第一个出错字段。
const inputs = new Map<string, HTMLInputElement>()
function registerInput(key: string) {
  return (el: Element | { $el?: unknown } | null) => {
    if (el instanceof HTMLInputElement) inputs.set(key, el)
    else inputs.delete(key)
  }
}

// 服务端字段错误集合：频带级（level/attenuation）用于输入框定位。
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

// 提交时任何错误都会清除旧结果，绝不展示上一次或部分计算。
function clearResultAndErrors() {
  result.value = null
  serverFields.value = []
  bannerErrors.value = []
  serverError.value = ''
}

function firstInvalidKey(): string | null {
  for (const f of FREQUENCIES) {
    if (fieldErrorSet.value.has(`${f}:level`)) return `${f}:level`
    if (fieldErrorSet.value.has(`${f}:attenuation`)) return `${f}:attenuation`
  }
  return null
}

// 用户修改任一输入时，清除该字段的服务端错误与旧结果：
// 一旦数据被改动，旧结果即不再对应当前表单。
function onInput(key: string) {
  const [fs, field] = key.split(':')
  const f = Number(fs)
  serverFields.value = serverFields.value.filter((e) => !(e.frequency === f && e.field === field))
  if (result.value) result.value = null
}

async function onSubmit() {
  clearResultAndErrors()

  // 空值以 null 提交；是否合法一律由 Go API 整单裁决，前端不做提交拦截。
  const payload = {
    bands: rows.map((r) => ({
      frequency: r.frequency,
      level: parseNumber(r.level),
      attenuation: parseNumber(r.attenuation)
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
      focusFirst()
    } else {
      serverError.value = `无法连接核算服务：${e instanceof Error ? e.message : String(e)}`
    }
  } finally {
    submitting.value = false
  }
}

function focusFirst() {
  const key = firstInvalidKey()
  if (key) {
    inputs.get(key)?.focus()
    inputs.get(key)?.select()
  }
}

function resetForm() {
  for (const r of rows) {
    r.level = ''
    r.attenuation = ''
  }
  clearResultAndErrors()
}

// 结果区逐行 C 值直接使用 API 行数据，避免任何本地重算。
const resultRows = computed(() => result.value?.rows ?? [])
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

    <form novalidate @submit.prevent="onSubmit">
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
          <tr v-for="row in rows" :key="row.frequency">
            <td class="freq">{{ row.frequency }}</td>
            <td>
              <input
                :ref="registerInput(`${row.frequency}:level`)"
                v-model="row.level"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="40"
                max="140"
                :aria-invalid="fieldErrorSet.has(`${row.frequency}:level`)"
                aria-label="现场声级 L"
                @input="onInput(`${row.frequency}:level`)"
              />
              <p
                v-if="fieldMessages.has(`${row.frequency}:level`)"
                class="field-error"
                role="alert"
              >
                {{ fieldMessages.get(`${row.frequency}:level`) }}
              </p>
            </td>
            <td>
              <input
                :ref="registerInput(`${row.frequency}:attenuation`)"
                v-model="row.attenuation"
                type="number"
                inputmode="decimal"
                step="0.1"
                min="0"
                max="40"
                :aria-invalid="fieldErrorSet.has(`${row.frequency}:attenuation`)"
                aria-label="耳罩衰减 A"
                @input="onInput(`${row.frequency}:attenuation`)"
              />
              <p
                v-if="fieldMessages.has(`${row.frequency}:attenuation`)"
                class="field-error"
                role="alert"
              >
                {{ fieldMessages.get(`${row.frequency}:attenuation`) }}
              </p>
            </td>
            <td class="corrected">
              <template v-if="result">
                {{ formatTenths(resultRows.find((r) => r.frequency === row.frequency)?.corrected ?? NaN) }}
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

    <section v-if="result" class="result" data-testid="result" aria-live="polite">
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
  </main>
</template>

<style scoped>
* {
  box-sizing: border-box;
}
.page {
  max-width: 960px;
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
</style>
