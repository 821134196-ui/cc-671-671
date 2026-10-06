<template>
  <div v-for="(c, i) in conflicts" :key="i" class="alert error">
    <span class="code">{{ codeLabel(c.code) }}</span>{{ c.message }}
  </div>
</template>

<script setup>
import { CONFLICT_TEXT } from '../utils.js'
defineProps({ conflicts: { type: Array, default: () => [] } })
function codeLabel(c) { return CONFLICT_TEXT[c] || c }
</script>
