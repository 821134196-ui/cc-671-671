<template>
  <div class="page-head">
    <div>
      <h1>散场逐件清点</h1>
      <div class="sub">散场后按实物逐件清点：完好 → 送洗护；破损 → 维修处理；未找到 → 少件事件单。每件同场只能清点一次，不能靠补数量抹平。</div>
    </div>
  </div>

  <div class="pill-tabs">
    <button v-for="p in performances" :key="p.id" :class="{ active: perfId === p.id }" @click="selectPerf(p.id)">
      {{ p.name }} · {{ fmtHM(p.show_time) }}
    </button>
  </div>

  <template v-if="detail">
    <div class="grid grid-4" style="margin-bottom:16px;">
      <div class="stat"><div class="n">{{ items.length }}</div><div class="l">应交回件数</div></div>
      <div class="stat ok"><div class="n">{{ checkedOk }}</div><div class="l">已清点·完好（待洗护）</div></div>
      <div class="stat danger"><div class="n">{{ damagedCount }}</div><div class="l">破损</div></div>
      <div class="stat danger"><div class="n">{{ lostCount }}</div><div class="l">少件</div></div>
    </div>

    <div class="card">
      <h2>逐件清点 <span class="hint">点击条码可直接登记</span></h2>
      <div style="overflow-x:auto;">
        <table>
          <thead>
            <tr>
              <th>实物编号</th><th>名称</th><th>实时状态</th><th>当前位置</th>
              <th>清点结果</th><th>清点时间</th><th style="width:260px;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in items" :key="it.costume.id">
              <td class="mono" style="color:var(--gold);">{{ it.costume.code }}</td>
              <td>{{ it.costume.name }}</td>
              <td><span class="badge" :class="it.costume.status">{{ statusText(it.costume.status) }}</span></td>
              <td class="location">📍 {{ it.costume.current_location }}</td>
              <td>
                <span v-if="!it.check" class="muted">未清点</span>
                <span v-else-if="!it.check.found" class="check-bad">⛔ 少件（事件 #{{ it.check.incident_id }}）</span>
                <span v-else-if="it.check.condition === 'damaged'" class="check-bad">⚠ 破损（事件 #{{ it.check.incident_id }}）</span>
                <span v-else class="check-ok">✓ 完好，已送洗护队列</span>
              </td>
              <td class="nowrap muted">{{ it.check ? fmtTime(it.check.checked_at) : '—' }}</td>
              <td>
                <template v-if="!it.check">
                  <button class="btn sm primary" @click="doCheck(it.costume, true, 'ok')">✓ 完好</button>
                  <button class="btn sm" @click="doCheck(it.costume, true, 'damaged')">⚠ 破损</button>
                  <button class="btn sm danger" @click="doCheck(it.costume, false, '')">✕ 未找到</button>
                </template>
                <span v-else class="muted" style="font-size:12px;">已清点，不可重复登记</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!items.length" class="empty">该场次暂无交接服装。</div>
    </div>

    <div class="card">
      <h2>清点记录</h2>
      <table>
        <thead><tr><th>时间</th><th>编号</th><th>服装</th><th>结果</th><th>备注</th><th>事件单号</th></tr></thead>
        <tbody>
          <tr v-for="c in detail.checks" :key="c.id">
            <td class="nowrap">{{ fmtTime(c.checked_at) }}</td>
            <td class="mono">{{ c.costume.code }}</td>
            <td>{{ c.costume.name }}</td>
            <td>
              <span v-if="!c.found" class="check-bad">少件</span>
              <span v-else-if="c.condition === 'damaged'" class="check-bad">破损</span>
              <span v-else class="check-ok">完好</span>
            </td>
            <td class="muted">{{ c.note }}</td>
            <td class="mono">{{ c.incident_id || '—' }}</td>
          </tr>
          <tr v-if="!detail.checks.length"><td colspan="6" class="muted" style="text-align:center;">尚无清点记录</td></tr>
        </tbody>
      </table>
    </div>
  </template>
</template>

<script setup>
import { inject, computed, onMounted, ref, watch } from 'vue'
import { api } from '../api.js'
import { fmtHM, fmtTime, statusText } from '../utils.js'
import { toast, toastError } from '../toast.js'

const rev = inject('rev')
const bump = inject('bump')

const performances = ref([])
const perfId = ref(null)
const detail = ref(null)

const items = computed(() => {
  if (!detail.value) return []
  const checks = new Map(detail.value.checks.map((c) => [c.costume_id, c]))
  const map = new Map()
  for (const route of detail.value.costumes) {
    map.set(route.costume.id, { costume: route.costume, check: checks.get(route.costume.id) || null })
  }
  return [...map.values()].sort((a, b) => a.costume.code.localeCompare(b.costume.code))
})

const checkedOk = computed(() => items.value.filter((i) => i.check?.found && i.check.condition === 'ok').length)
const damagedCount = computed(() => items.value.filter((i) => i.check?.condition === 'damaged').length)
const lostCount = computed(() => items.value.filter((i) => i.check && !i.check.found).length)

async function loadPerfs() {
  performances.value = await api.performances()
  if (!perfId.value && performances.value.length) perfId.value = performances.value[0].id
}

async function loadDetail() {
  if (perfId.value) detail.value = await api.performance(perfId.value)
}

function selectPerf(id) { perfId.value = id }

async function doCheck(costume, found, condition) {
  let note = ''
  if (!found) note = window.prompt(`登记少件：${costume.code} ${costume.name}，请备注最后见到位置（可留空）`)
  else if (condition === 'damaged') note = window.prompt(`登记破损：${costume.code}，请描述破损情况`)
  if (note === null) return // 用户取消
  try {
    const res = await api.check(perfId.value, { costume_id: costume.id, found, condition, note: note || '' })
    if (res.incident) {
      toast(`已开${res.incident.type === 'lost' ? '少件' : '破损'}事件单 #${res.incident.id}，请到“服装资产”页处理`, 'error')
    } else {
      toast('清点为完好，已转入待洗护，洗护完成后才能再次使用')
    }
    bump()
    await loadDetail()
  } catch (e) { toastError(e) }
}

onMounted(async () => { await loadPerfs(); await loadDetail() })
watch(perfId, loadDetail)
watch(rev, loadDetail)
</script>
