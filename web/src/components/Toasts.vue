<template>
  <div class="toast-wrap">
    <div v-for="t in items" :key="t.id" class="toast" :class="t.type">
      <div>{{ t.message }}</div>
      <div v-if="t.details && t.details.length" style="margin-top:6px; display:flex; flex-direction:column; gap:4px;">
        <div v-for="(d, i) in t.details" :key="i" style="font-size:12px; opacity:0.92;">
          <span class="mono" style="opacity:0.7;">[{{ d.code }}]</span> {{ d.message }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { toastState } from '../toast.js'
const items = toastState.items
</script>
