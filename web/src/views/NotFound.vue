<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="nf-page">
    <div class="nf-code">404</div>
    <p class="nf-text">This page does not exist.</p>
    <button class="btn btn-primary" @click="router.push('/')">Back to dashboard</button>
  </div>
</template>

<style scoped>
.nf-page {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  background: var(--bg-secondary);
}

.nf-code {
  font-size: 64px;
  font-weight: 700;
  color: var(--text-muted);
}

.nf-text {
  color: var(--text-secondary);
}
</style>
