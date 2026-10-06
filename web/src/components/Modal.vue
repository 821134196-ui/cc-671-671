<template>
  <div class="modal-mask" @click.self="$emit('close')">
    <div class="modal" :class="{ wide }">
      <header>
        <span>{{ title }}</span>
        <button class="close-x" @click="$emit('close')">×</button>
      </header>
      <div class="body"><slot /></div>
      <footer v-if="$slots.footer">
        <slot name="footer" />
      </footer>
    </div>
  </div>
</template>

<script setup>
defineProps({ title: String, wide: Boolean })
defineEmits(['close'])
</script>
