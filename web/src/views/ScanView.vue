<template>
  <div class="page-head">
    <div>
      <h1>扫码交接（本地模拟扫码枪）</h1>
      <div class="sub">扫描或输入服装实物编号，完成“演员签收穿上 / 散场交回”。未经洗护或存在未处理事件的衣服扫码即被拦截。</div>
    </div>
  </div>

  <div class="grid grid-2">
    <div class="card">
      <h2>📷 扫码 / 输入条码</h2>
      <div class="scan-box">
        <input
          ref="codeInput"
          v-model="code"
          placeholder="如 C001"
          @keyup.enter="doScan('lookup')"
        />
        <button class="btn" @click="doScan('lookup')">查询</button>
      </div>
      <div class="form-row" style="margin-top:14px;">
        <div v-for="c in quickCodes" :key="c.code">
          <button class="btn ghost sm" style="width:100%; justify-content:center;" @click="quickScan(c.code)">{{ c.code }}</button>
        </div>
      </div>

      <div style="display:flex; gap:10px; margin-top:18px;">
        <button class="btn primary" style="flex:1;" @click="doScan('sign')">✅ 签收穿上</button>
        <button class="btn" style="flex:1;" @click="doScan('collect')">📥 散场交回</button>
      </div>
      <p class="muted" style="font-size:12px; margin-top:10px;">
        提示：模拟流程为“查询定位 → 演员签收 → 散场交回 → 清点洗护”。已签收的衣服不能重复签收。
      </p>
    </div>

    <div class="card" :class="{ flash: flashKey }" :key="flashKey">
      <h2>扫码结果</h2>
      <div v-if="!result" class="empty">扫描服装条码后，这里显示当前位置、状态与下一位使用人。</div>
      <template v-else>
        <div class="alert ok">{{ result.message }}</div>
        <table>
          <tbody>
            <tr><th style="width:110px;">实物编号</th><td class="mono" style="color:var(--gold);">{{ result.costume.code }}</td></tr>
            <tr><th>名称</th><td>{{ result.costume.name }}</td></tr>
            <tr><th>当前状态</th><td><span class="badge" :class="result.costume.status">{{ statusText(result.costume.status) }}</span></td></tr>
            <tr><th>当前位置</th><td class="location">📍 {{ result.costume.current_location }}</td></tr>
            <tr v-if="result.assignment">
              <th>交接任务</th>
              <td>
                {{ result.assignment.scene }} · {{ result.assignment.actor?.name }}
                <span class="badge" :class="result.assignment.status" style="margin-left:6px;">
                  {{ statusText(result.assignment.status) }}
                </span>
              </td>
            </tr>
            <tr v-if="result.next_user">
              <th>下一位使用人</th>
              <td style="color:var(--gold-soft);">
                👉 {{ result.next_user.actor_name }} · {{ result.next_user.scene }} ·
                {{ fmtTime(result.next_user.handover_at) }} · {{ result.next_user.change_point }}
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>
  </div>

  <div class="card">
    <h2>待交接任务 <span class="hint">按交接时间排序，可直接定位条码</span></h2>
    <div style="overflow-x:auto;">
      <table>
        <thead>
          <tr><th>交接时间</th><th>场景</th><th>服装条码</th><th>服装</th><th>演员</th><th>换装点</th><th>状态</th><th></th></tr>
        </thead>
        <tbody>
          <tr v-for="a in pendingList" :key="a.id">
            <td class="nowrap">{{ fmtTime(a.handover_at) }}</td>
            <td>{{ a.scene }}</td>
            <td class="mono" style="color:var(--gold);">{{ a.costume?.code }}</td>
            <td>{{ a.costume?.name }}</td>
            <td>{{ a.actor?.name }}</td>
            <td>{{ a.change_point }}</td>
            <td><span class="badge" :class="a.status">{{ statusText(a.status) }}</span></td>
            <td><button class="btn sm" @click="code = a.costume?.code; codeInput?.focus()">取条码</button></td>
          </tr>
          <tr v-if="!pendingList.length"><td colspan="8" class="muted" style="text-align:center;">暂无交接任务</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { inject, onMounted, ref, watch } from 'vue'
import { api } from '../api.js'
import { fmtTime, statusText } from '../utils.js'
import { toast, toastError } from '../toast.js'

const rev = inject('rev')
const bump = inject('bump')

const code = ref('')
const result = ref(null)
const flashKey = ref(0)
const pendingList = ref([])
const quickCodes = ref([])
const codeInput = ref(null)

async function loadPending() {
  const perfs = await api.performances()
  if (!perfs.length) { pendingList.value = []; return }
  const d = await api.performance(perfs[0].id)
  pendingList.value = d.assignments
    .filter((a) => a.status === 'pending' || a.status === 'signed')
    .sort((x, y) => new Date(x.handover_at) - new Date(y.handover_at))
  quickCodes.value = [...new Map(pendingList.value.map((a) => [a.costume?.code, { code: a.costume?.code }])).values()].slice(0, 8)
}

async function doScan(action) {
  if (!code.value.trim()) return toast('请扫描或输入服装条码', 'error')
  try {
    result.value = await api.scan({ code: code.value.trim(), action, operator: '服装师' })
    flashKey.value++
    if (action !== 'lookup') { bump(); await loadPending() }
  } catch (e) {
    result.value = null
    toastError(e)
  }
}

function quickScan(c) { code.value = c; doScan('lookup') }

onMounted(loadPending)
watch(rev, loadPending)
</script>
