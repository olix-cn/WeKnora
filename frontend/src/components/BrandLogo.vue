<template>
  <img class="brand-logo" :class="{ 'brand-logo--on-dark': onDark }" src="/brand/aethcraft-light.png" alt="AethCraft" />
</template>

<script setup lang="ts">
defineProps<{ onDark?: boolean }>()
</script>

<style>
.brand-logo {
  display: block;
  width: 148px;
  max-width: 100%;
  height: auto;
  object-fit: contain;
}
html[theme-mode="dark"] .brand-logo,
.brand-logo--on-dark {
  content: url('/brand/aethcraft-dark.png');
}
</style>
