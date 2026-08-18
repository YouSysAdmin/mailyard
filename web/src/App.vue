<script setup lang="ts">
// The root: the routed page, plus the two surfaces that outlive it.
//
// The confirm dialog and the toast stack are mounted HERE rather than
// per view because both are driven by global state and answer requests
// raised from anywhere - a view that owned them would take them down
// with it on navigation, cancelling a prompt somebody was reading.
//
// Instantiating the theme store is a side effect and the point of the
// call: constructing it resolves the stored mode and stamps
// `data-theme` on the document before the first page paints.
import ConfirmDialog from './components/ConfirmDialog.vue'
import ToastStack from './components/ToastStack.vue'
import { useThemeStore } from './stores/theme'

useThemeStore()
</script>

<template>
  <router-view />
  <ConfirmDialog />
  <ToastStack />
</template>
