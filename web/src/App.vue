<template>
  <div id="app">
    <header class="topbar">
      <div class="brand">
        <span class="mask">🎭</span>
        <div>
          剧院服装调度台
          <small v-if="currentPerf" style="margin-left:10px;">{{ currentPerf.name }}</small>
        </div>
      </div>
      <nav class="nav">
        <a href="#/performances" :class="{ active: tab === 'performances' }">场次路线</a>
        <a href="#/costumes" :class="{ active: tab === 'costumes' }">服装资产</a>
        <a href="#/scan" :class="{ active: tab === 'scan' }">扫码交接</a>
        <a href="#/checkout" :class="{ active: tab === 'checkout' }">散场清点</a>
      </nav>
    </header>

    <main>
      <PerformancesView v-if="tab === 'performances'" @open-scan="goScan" />
      <CostumesView v-else-if="tab === 'costumes'" />
      <ScanView v-else-if="tab === 'scan'" @changed="onChanged" />
      <CheckoutView v-else-if="tab === 'checkout'" @changed="onChanged" />
    </main>

    <Toasts />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, provide } from 'vue'
import PerformancesView from './views/PerformancesView.vue'
import CostumesView from './views/CostumesView.vue'
import ScanView from './views/ScanView.vue'
import CheckoutView from './views/CheckoutView.vue'
import Toasts from './components/Toasts.vue'
import { api } from './api.js'

const tab = ref('performances')
const currentPerf = ref(null)

// 全局数据版本号：任何写操作后 bump，各页面 watch 后刷新。
const rev = ref(0)
function bump() { rev.value++ }
provide('rev', rev)
provide('bump', bump)

function parseHash() {
  const h = location.hash.replace(/^#\/?/, '')
  const [name] = h.split('/')
  tab.value = ['performances', 'costumes', 'scan', 'checkout'].includes(name) ? name : 'performances'
}

function goScan() { location.hash = '#/scan' }
function onChanged() { bump() }

async function refreshPerfName() {
  const m = location.hash.match(/performances\/(\d+)/)
  if (m) {
    try { currentPerf.value = await api.performance(m[1]).then((d) => d.performance) } catch { currentPerf.value = null }
  } else {
    currentPerf.value = null
  }
}

function onHash() { parseHash(); refreshPerfName() }
onMounted(() => {
  window.addEventListener('hashchange', onHash)
  parseHash()
})
onUnmounted(() => window.removeEventListener('hashchange', onHash))
</script>
