<script setup lang="ts">
// A hidden username field, for the password manager's benefit.
//
// A manager associates a credential with an ACCOUNT, so a password
// field standing on its own has nothing to file itself under: the
// entry is saved against the wrong site, or silently not saved at all.
// Every dialog here asking for a password carries one of these.
//
// A component rather than eight lines copied per dialog, because what
// it is for is not visible from what it does - and an invisible input
// with no obvious purpose is exactly what gets deleted in a cleanup.
defineProps<{ email?: string }>()
</script>

<template>
  <input
    type="text"
    name="username"
    autocomplete="username"
    :value="email"
    class="hidden"
    readonly
  />
</template>
