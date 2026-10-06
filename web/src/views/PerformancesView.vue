<template>
  <div class="page-head">
    <div>
      <h1>场次换装路线</h1>
      <div class="sub">按上场顺序查看每件服装的位置、下一位使用人，提前发现来不及换装 / 衣服不到位 / 清洁间隔冲突。</div>
    </div>
    <div style="display:flex; gap:10px;">
      <button class="btn" @click="showActorModal = true">＋ 演员</button>
      <button class="btn" @click="showPerfModal = true">＋ 场次</button>
    </div>
  </div>

  <div class="pill-tabs">
    <button v-for="p in performances" :key="p.id" :class="{ active: perfId === p.id }" @click="selectPerf(p.id)">
      {{ p.name }} · {{ fmtHM(p.show_time) }}
    </button>
  </div>

  <div v-if="!detail" class="empty">暂无场次，点击右上角“＋ 场次”创建。</div>

  <template v-else>
    <!-- 汇总 -->
    <div class="grid grid-4" style="margin-bottom:16px;">
      <div class="stat"><div class="n">{{ detail.summary.assignments }}</div><div class="l">交接任务总数</div></div>
      <div class="stat gold"><div class="n">{{ detail.summary.pending }}</div><div class="l">待执行</div></div>
      <div class="stat ok"><div class="n">{{ detail.summary.signed + detail.summary.collected }}</div><div class="l">已签收 / 已交回</div></div>
      <div class="stat" :class="{ danger: detail.summary.conflicts }">
        <div class="n">{{ detail.summary.conflicts }}</div><div class="l">路线冲突</div>
      </div>
    </div>

    <!-- 冲突总览 -->
    <div class="card">
      <h2>⚠️ 时间与到位冲突 <span class="hint">演出前必须逐条解决</span></h2>
      <div v-if="!detail.conflicts.length" class="alert ok">✓ 当前换装路线未发现冲突，服装均可按时到位。</div>
      <ConflictAlerts :conflicts="detail.conflicts" />
      <div style="margin-top:12px;">
        <button class="btn primary sm" @click="openAssign">＋ 安排换装交接</button>
      </div>
    </div>

    <!-- 每件服装的路线 -->
    <div class="card" v-for="route in detail.costumes" :key="route.costume.id">
      <h2 style="justify-content:space-between;">
        <span>
          <span class="mono" style="color:var(--gold);">{{ route.costume.code }}</span>
          {{ route.costume.name }}
          <span class="badge" :class="route.costume.status">{{ statusText(route.costume.status) }}</span>
        </span>
        <span class="location">📍 {{ route.costume.current_location }}</span>
      </h2>

      <div style="display:flex; gap:24px; flex-wrap:wrap; margin-bottom:12px; font-size:13px;">
        <div>
          <span class="muted">预计换装：</span>{{ fmtDuration(route.costume.change_seconds) }}
        </div>
        <div>
          <span class="muted">清洁间隔：</span>{{ fmtDuration(route.costume.clean_seconds) }}
        </div>
        <div v-if="route.next_user" style="color:var(--gold-soft);">
          👉 下一位：{{ route.next_user.actor_name }}（{{ route.next_user.scene }}，
          {{ fmtTime(route.next_user.handover_at) }} 在 {{ route.next_user.change_point }} 交接）
        </div>
        <div v-else class="muted">无后续使用人</div>
      </div>

      <div class="route">
        <template v-for="(leg, i) in route.legs" :key="leg.id">
          <div class="leg" :class="{ 'has-conflict': legConflicts(leg.id).length }">
            <div class="scene">
              <span class="badge" :class="leg.status">{{ statusText(leg.status) }}</span>
              <span style="margin-left:6px;">{{ leg.scene }}</span>
            </div>
            <div class="who">👤 {{ leg.actor.name }}<span class="muted">（{{ leg.actor.role }}）</span></div>
            <div class="meta">🔄 {{ leg.change_point }} · 顺序 #{{ leg.sequence }}</div>
            <div class="times">
              交接 {{ fmtHM(leg.handover_at) }} → 上场 {{ fmtHM(leg.need_by_at) }}<br />
              交回 {{ fmtHM(leg.return_at) }}
              <span v-if="leg.planned_actor_id !== leg.actor_id" class="muted" style="display:block; margin-top:2px;">
                原定 #{{ leg.planned_actor_id }}，已临时换角
              </span>
            </div>
            <div v-for="cf in legConflicts(leg.id)" :key="cf.code" class="conflict-tag">⛔ {{ cf.message }}</div>
          </div>
          <div v-if="i < route.legs.length - 1" class="leg-arrow">➜</div>
        </template>
      </div>
    </div>

    <!-- 交接任务表 -->
    <div class="card">
      <h2>交接任务明细 <span class="hint">临时换角只作用于“待执行”任务，已签收记录锁定原演员</span></h2>
      <div style="overflow-x:auto;">
        <table>
          <thead>
            <tr>
              <th>#</th><th>场次</th><th>服装</th><th>演员</th><th>换装点</th>
              <th>交接</th><th>上场</th><th>交回</th><th>状态</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in detail.assignments" :key="a.id">
              <td class="mono">{{ a.sequence }}</td>
              <td>{{ a.scene }}</td>
              <td><span class="mono">{{ a.costume.code }}</span> {{ a.costume.name }}</td>
              <td>
                {{ a.actor.name }}
                <div v-if="a.planned_actor_id !== a.actor_id" class="muted" style="font-size:11px;">
                  原定演员 ID {{ a.planned_actor_id }}
                </div>
              </td>
              <td>{{ a.change_point }}</td>
              <td class="nowrap">{{ fmtHM(a.handover_at) }}</td>
              <td class="nowrap">{{ fmtHM(a.need_by_at) }}</td>
              <td class="nowrap">{{ fmtHM(a.return_at) }}</td>
              <td><span class="badge" :class="a.status">{{ statusText(a.status) }}</span></td>
              <td class="nowrap">
                <button v-if="a.status === 'pending'" class="btn sm" @click="openRecast(a)">临时换角</button>
                <span v-else-if="a.status === 'signed'" class="muted" style="font-size:11px;">签收锁定</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </template>

  <!-- 新建场次 -->
  <Modal v-if="showPerfModal" title="新建演出场次" @close="showPerfModal = false">
    <div class="field"><label>名称</label><input v-model="perfForm.name" placeholder="如：《夜色庄园》第二场" /></div>
    <div class="form-row">
      <div><label>剧场</label><input v-model="perfForm.venue" /></div>
      <div><label>开场时间</label><input type="datetime-local" v-model="perfForm.show_time" /></div>
    </div>
    <template #footer>
      <button class="btn" @click="showPerfModal = false">取消</button>
      <button class="btn primary" @click="savePerf">创建</button>
    </template>
  </Modal>

  <!-- 新建演员 -->
  <Modal v-if="showActorModal" title="登记演员 / 角色" @close="showActorModal = false">
    <div class="form-row">
      <div><label>姓名</label><input v-model="actorForm.name" /></div>
      <div><label>角色</label><input v-model="actorForm.role" /></div>
    </div>
    <div class="field"><label>联系电话</label><input v-model="actorForm.phone" /></div>
    <template #footer>
      <button class="btn" @click="showActorModal = false">取消</button>
      <button class="btn primary" @click="saveActor">保存</button>
    </template>
  </Modal>

  <!-- 安排交接 -->
  <Modal v-if="showAssignModal" title="安排换装交接" wide @close="showAssignModal = false">
    <div class="alert warn" v-if="forceAssign">
      已勾选“忽略冲突强行排单”。请确认服装师已现场解决到位问题。
    </div>
    <div class="form-row">
      <div>
        <label>服装（实物编号）</label>
        <select v-model.number="assignForm.costume_id">
          <option v-for="c in costumes" :key="c.id" :value="c.id">
            {{ c.code }} · {{ c.name }}（{{ statusText(c.status) }}）
          </option>
        </select>
      </div>
      <div>
        <label>演员</label>
        <select v-model.number="assignForm.actor_id">
          <option v-for="a in actors" :key="a.id" :value="a.id">{{ a.name }} · {{ a.role }}</option>
        </select>
      </div>
    </div>
    <div class="form-row">
      <div><label>场景</label><input v-model="assignForm.scene" placeholder="第二幕" /></div>
      <div><label>上场顺序</label><input type="number" v-model.number="assignForm.sequence" /></div>
      <div><label>换装点</label><input v-model="assignForm.change_point" placeholder="上场门换装点 1" /></div>
    </div>
    <div class="form-row">
      <div><label>计划交接（签收）时间</label><input type="datetime-local" v-model="assignForm.handover_at" /></div>
      <div><label>必须穿好上场时间</label><input type="datetime-local" v-model="assignForm.need_by_at" /></div>
      <div><label>计划交回时间</label><input type="datetime-local" v-model="assignForm.return_at" /></div>
    </div>
    <label style="display:flex; align-items:center; gap:8px; color:var(--text); margin-top:6px;">
      <input type="checkbox" v-model="forceAssign" style="width:auto;" /> 明知有冲突仍强行排单（需服装主管确认）
    </label>
    <template #footer>
      <button class="btn" @click="showAssignModal = false">取消</button>
      <button class="btn primary" @click="saveAssign">安排</button>
    </template>
  </Modal>

  <!-- 临时换角 -->
  <Modal v-if="recastTarget" title="临时换角" @close="recastTarget = null">
    <div class="alert warn">
      仅调整尚未执行的任务。已签收的记录仍关联原演员，不能更改。
    </div>
    <div class="field">
      <label>任务</label>
      <div>{{ recastTarget.scene }} · {{ recastTarget.costume.name }} · 原定 {{ recastTarget.actor.name }}</div>
    </div>
    <div class="field">
      <label>改由哪位演员出演</label>
      <select v-model.number="recastActor">
        <option v-for="a in actors.filter((x) => x.id !== recastTarget.actor_id)" :key="a.id" :value="a.id">
          {{ a.name }} · {{ a.role }}
        </option>
      </select>
    </div>
    <div class="field"><label>原因</label><input v-model="recastReason" placeholder="如：身体不适" /></div>
    <template #footer>
      <button class="btn" @click="recastTarget = null">取消</button>
      <button class="btn primary" @click="saveRecast">确认换角</button>
    </template>
  </Modal>
</template>

<script setup>
import { inject, onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api.js'
import { fmtHM, fmtTime, fmtDuration, statusText, toLocalInput } from '../utils.js'
import { toast, toastError } from '../toast.js'
import Modal from '../components/Modal.vue'
import ConflictAlerts from '../components/ConflictAlerts.vue'

const rev = inject('rev')
const bump = inject('bump')

const performances = ref([])
const perfId = ref(null)
const detail = ref(null)
const costumes = ref([])
const actors = ref([])

const showPerfModal = ref(false)
const showActorModal = ref(false)
const showAssignModal = ref(false)
const forceAssign = ref(false)
const recastTarget = ref(null)
const recastActor = ref(null)
const recastReason = ref('')

const perfForm = reactive({ name: '', venue: '', show_time: toLocalInput() })
const actorForm = reactive({ name: '', role: '', phone: '' })
const assignForm = reactive({
  costume_id: null, actor_id: null, scene: '', sequence: 1, change_point: '',
  handover_at: toLocalInput(), need_by_at: toLocalInput(), return_at: toLocalInput()
})

async function loadLists() {
  performances.value = await api.performances()
  costumes.value = await api.costumes()
  actors.value = await api.actors()
  if (!perfId.value && performances.value.length) perfId.value = performances.value[0].id
}

async function loadDetail() {
  if (!perfId.value) { detail.value = null; return }
  detail.value = await api.performance(perfId.value)
}

function selectPerf(id) { perfId.value = id }

function legConflicts(id) {
  return (detail.value?.conflicts || []).filter(
    (c) => c.assignment_id === id || (c.related_ids || []).includes(id)
  )
}

function openAssign() {
  Object.assign(assignForm, {
    performance_id: perfId.value, costume_id: costumes.value[0]?.id, actor_id: actors.value[0]?.id,
    scene: '', sequence: (detail.value?.assignments.length || 0) + 1, change_point: '',
    handover_at: toLocalInput(detail.value?.performance.show_time),
    need_by_at: toLocalInput(detail.value?.performance.show_time),
    return_at: toLocalInput(detail.value?.performance.show_time)
  })
  forceAssign.value = false
  showAssignModal.value = true
}

async function saveAssign() {
  try {
    const res = await api.createAssignment({ ...assignForm, performance_id: perfId.value, force: forceAssign.value })
    showAssignModal.value = false
    if (res.conflicts?.length) toast(`已排单，但仍有 ${res.conflicts.length} 条路线冲突需关注`, 'error', res.conflicts)
    else toast('换装交接已安排')
    await reload()
  } catch (e) { toastError(e) }
}

function openRecast(a) {
  recastTarget.value = a
  recastActor.value = actors.value.find((x) => x.id !== a.actor_id)?.id || null
  recastReason.value = ''
}

async function saveRecast() {
  try {
    const res = await api.recast(recastTarget.value.id, { actor_id: recastActor.value, reason: recastReason.value })
    recastTarget.value = null
    if (res.conflicts?.length) toast('换角成功，但新演员路线存在冲突', 'error', res.conflicts)
    else toast('临时换角成功，原定演员记录已保留')
    await reload()
  } catch (e) { toastError(e) }
}

async function savePerf() {
  if (!perfForm.name || !perfForm.show_time) return toast('请填写名称与开场时间', 'error')
  try {
    const p = await api.createPerformance({ ...perfForm })
    showPerfModal.value = false
    perfId.value = p.id
    toast('场次已创建')
    await reload()
  } catch (e) { toastError(e) }
}

async function saveActor() {
  if (!actorForm.name) return toast('请填写姓名', 'error')
  try {
    await api.createActor({ ...actorForm })
    showActorModal.value = false
    actors.value = await api.actors()
    toast('演员已登记')
  } catch (e) { toastError(e) }
}

async function reload() {
  await loadLists()
  await loadDetail()
}

onMounted(reload)
watch(perfId, loadDetail)
watch(rev, reload)
</script>
