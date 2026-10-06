<template>
  <div class="page-head">
    <div>
      <h1>服装资产</h1>
      <div class="sub">每件实物独立编号、独立追溯。洗护或维修完成后才能恢复可用；少件/破损必须走事件处理单。</div>
    </div>
    <button class="btn primary" @click="showCreate = true">＋ 登记服装</button>
  </div>

  <div class="card">
    <div class="pill-tabs">
      <button v-for="f in filters" :key="f.v" :class="{ active: filter === f.v }" @click="filter = f.v">
        {{ f.label }} <span class="muted">({{ countOf(f.v) }})</span>
      </button>
    </div>

    <div style="overflow-x:auto;">
      <table>
        <thead>
          <tr>
            <th>实物编号</th><th>名称</th><th>分类</th><th>状态</th>
            <th>当前位置</th><th>换装耗时</th><th>清洁间隔</th><th>最近洗护</th><th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in filtered" :key="c.id">
            <td class="mono" style="color:var(--gold);">{{ c.code }}</td>
            <td>{{ c.name }}</td>
            <td class="muted">{{ c.category }}</td>
            <td><span class="badge" :class="c.status">{{ statusText(c.status) }}</span></td>
            <td class="location">📍 {{ c.current_location }}</td>
            <td class="nowrap">{{ fmtDuration(c.change_seconds) }}</td>
            <td class="nowrap">{{ fmtDuration(c.clean_seconds) }}</td>
            <td class="nowrap muted">{{ c.last_cleaned_at ? fmtTime(c.last_cleaned_at) : '—' }}</td>
            <td class="nowrap">
              <button class="btn sm" @click="openCare(c)">洗护/维修</button>
              <button class="btn sm" @click="openHistory(c)">记录</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- 少件/破损事件处理入口 -->
  <div class="card">
    <h2>🚨 少件 / 破损处理单 <span class="hint">不允许靠“补库存数量”消除问题</span></h2>
    <div v-if="!openIncidents.length" class="alert ok">✓ 没有待处理的少件/破损事件。</div>
    <table v-else>
      <thead>
        <tr><th>类型</th><th>服装</th><th>说明</th><th>报单时间</th><th>处理</th></tr>
      </thead>
      <tbody>
        <tr v-for="inc in openIncidents" :key="inc.id">
          <td>
            <span class="badge" :class="inc.type">{{ inc.type === 'lost' ? '少件' : '破损' }}</span>
          </td>
          <td><span class="mono">{{ inc.costume.code }}</span> {{ inc.costume.name }}</td>
          <td style="max-width:340px;">{{ inc.description }}</td>
          <td class="nowrap muted">{{ fmtTime(inc.reported_at) }}</td>
          <td class="nowrap">
            <button v-if="inc.type === 'lost'" class="btn sm" @click="resolve(inc, 'found')">已找回（仍需洗护）</button>
            <button v-if="inc.type === 'damaged'" class="btn sm primary" @click="resolve(inc, 'repaired')">维修完成恢复可用</button>
            <button class="btn sm danger" @click="resolve(inc, 'written_off')">报废</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <!-- 登记服装 -->
  <Modal v-if="showCreate" title="登记实物服装" @close="showCreate = false">
    <div class="form-row">
      <div><label>实物编号 / 条码 *</label><input v-model="form.code" placeholder="C006" /></div>
      <div><label>名称</label><input v-model="form.name" /></div>
      <div><label>分类</label><input v-model="form.category" /></div>
    </div>
    <div class="form-row">
      <div><label>预计换装耗时（秒）</label><input type="number" v-model.number="form.change_seconds" /></div>
      <div><label>最短清洁间隔（秒）</label><input type="number" v-model.number="form.clean_seconds" /></div>
      <div><label>初始位置</label><input v-model="form.current_location" placeholder="主换装间" /></div>
    </div>
    <template #footer>
      <button class="btn" @click="showCreate = false">取消</button>
      <button class="btn primary" @click="save">登记</button>
    </template>
  </Modal>

  <!-- 洗护/维修 -->
  <Modal v-if="careTarget" :title="`洗护 / 维修：${careTarget.code} ${careTarget.name}`" @close="careTarget = null">
    <div class="alert warn" v-if="careTarget.status === 'returned'">
      该衣服散场已交回，完成洗护后才能再次分配。
    </div>
    <div class="field">
      <label>操作类型</label>
      <select v-model="careForm.type">
        <option value="wash">洗护</option>
        <option value="repair">维修</option>
      </select>
    </div>
    <div class="field"><label>原因 / 备注</label><input v-model="careForm.reason" /></div>
    <div class="field"><label>操作人</label><input v-model="careForm.operator" placeholder="洗衣组 / 维修组" /></div>
    <template #footer>
      <button class="btn" @click="careTarget = null">取消</button>
      <button class="btn primary" @click="startCare">送出</button>
    </template>
  </Modal>

  <!-- 洗护/维修历史 -->
  <Modal v-if="historyTarget" :title="`洗护/维修记录：${historyTarget.code}`" wide @close="historyTarget = null">
    <div style="margin-bottom:14px;">
      <span class="badge" :class="historyTarget.status">{{ statusText(historyTarget.status) }}</span>
      <span class="location" style="margin-left:10px;">📍 {{ historyTarget.current_location }}</span>
    </div>
    <table>
      <thead><tr><th>类型</th><th>状态</th><th>原因</th><th>开始</th><th>完成</th><th>操作人</th><th></th></tr></thead>
      <tbody>
        <tr v-for="r in history" :key="r.id">
          <td>{{ r.type === 'wash' ? '洗护' : '维修' }}</td>
          <td>{{ r.status === 'done' ? '已完成' : '进行中' }}</td>
          <td>{{ r.reason }}</td>
          <td class="nowrap muted">{{ fmtTime(r.start_at) }}</td>
          <td class="nowrap muted">{{ r.completed_at ? fmtTime(r.completed_at) : '—' }}</td>
          <td>{{ r.operator }}</td>
          <td>
            <button v-if="r.status === 'ongoing'" class="btn sm primary" @click="completeCare(r)">完成，恢复可用</button>
          </td>
        </tr>
      </tbody>
    </table>
  </Modal>
</template>

<script setup>
import { inject, onMounted, reactive, ref, computed, watch } from 'vue'
import { api } from '../api.js'
import { fmtTime, fmtDuration, statusText } from '../utils.js'
import { toast, toastError } from '../toast.js'
import Modal from '../components/Modal.vue'

const rev = inject('rev')
const costumes = ref([])
const incidents = ref([])
const history = ref([])
const filter = ref('all')

const showCreate = ref(false)
const careTarget = ref(null)
const historyTarget = ref(null)

const form = reactive({ code: '', name: '', category: '', change_seconds: 180, clean_seconds: 1800, current_location: '' })
const careForm = reactive({ type: 'wash', reason: '', operator: '' })

const filters = [
  { v: 'all', label: '全部' },
  { v: 'available', label: '可用' },
  { v: 'returned', label: '待洗护' },
  { v: 'washing', label: '洗护中' },
  { v: 'repairing', label: '维修中' },
  { v: 'lost', label: '少件' },
  { v: 'damaged', label: '破损' },
  { v: 'retired', label: '已报废' }
]

const filtered = computed(() =>
  filter.value === 'all' ? costumes.value : costumes.value.filter((c) => c.status === filter.value)
)
const openIncidents = computed(() => incidents.value.filter((i) => i.status === 'open'))

function countOf(v) {
  return v === 'all' ? costumes.value.length : costumes.value.filter((c) => c.status === v).length
}

async function load() {
  costumes.value = await api.costumes()
  incidents.value = await api.incidents('open')
}

async function save() {
  if (!form.code) return toast('实物编号必填', 'error')
  try {
    await api.createCostume({ ...form })
    showCreate.value = false
    Object.assign(form, { code: '', name: '', category: '', change_seconds: 180, clean_seconds: 1800, current_location: '' })
    toast('服装已登记，状态为可用')
    await load()
  } catch (e) { toastError(e) }
}

function openCare(c) {
  careTarget.value = c
  careForm.type = c.status === 'damaged' ? 'repair' : 'wash'
  careForm.reason = ''
  careForm.operator = ''
}

async function startCare() {
  try {
    await api.startCare(careTarget.value.id, { ...careForm })
    careTarget.value = null
    toast(careForm.type === 'wash' ? '已送洗护，完成后恢复可用' : '已送维修，完成后恢复可用')
    await load()
  } catch (e) { toastError(e) }
}

async function openHistory(c) {
  historyTarget.value = c
  history.value = await api.care(c.id)
}

async function completeCare(r) {
  try {
    const res = await api.completeCare(r.id, { operator: r.operator || '服装组' })
    toast(res.message || '已恢复可用')
    historyTarget.value = res.costume
    history.value = await api.care(r.costume_id)
    await load()
  } catch (e) { toastError(e) }
}

async function resolve(inc, resolution) {
  const labels = { found: '确认找回（找回后仍须洗护）', repaired: '确认维修完成并恢复可用', written_off: '确认报废该服装' }
  if (!window.confirm(labels[resolution])) return
  try {
    const res = await api.resolveIncident(inc.id, {
      resolution,
      handler: '服装主管',
      note: resolution === 'found' ? '找回待洗护' : resolution === 'repaired' ? '修复检验合格' : '无法修复'
    })
    toast(`事件已处理：${res.costume.name} → ${statusText(res.costume.status)}`)
    await load()
  } catch (e) { toastError(e) }
}

onMounted(load)
watch(rev, load)
</script>
