<script setup lang="ts">
// The signed-out shell: a centred card on an empty page, with the mark
// and a heading above whatever the page is asking for.
//
// ONE component because there were five copies - sign in, register,
// forgot, reset and confirm - and they had drifted in exactly the way
// five copies do. The mark was 48px on two pages and 52px on three,
// because two of them pinned a width in CSS while all five ASKED the
// component for 52; and it took the card's text colour on three and was
// left to inherit on the other two. Nobody decided any of that.
//
// The size lives here and nowhere else now. A width in CSS beats the
// svg's own width attribute, which is why the number the pages passed
// was quietly losing.
import BrandMark from './BrandMark.vue'

defineProps<{ title: string }>()
</script>

<template>
  <div class="page">
    <div class="card sheet">
      <header class="head">
        <BrandMark class="mark" :size="52" />
        <h1>{{ title }}</h1>
      </header>

      <slot />
    </div>
  </div>
</template>

<style scoped>
/* The whole viewport, because there is no console around this yet -
   no rail, no bar, nothing to sit inside. */
.page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 24px;
  background: var(--bg-secondary);
}

/* Narrow on purpose: every one of these asks for two fields at most,
   and a wide form for two fields reads as a page that lost its content. */
.sheet {
  width: 100%;
  max-width: 380px;
  padding: 32px;
}

.head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}

.head h1 {
  color: var(--text-primary);
  font-size: 18px;
  font-weight: 600;
}

/* A line mark, so it takes the card's text colour and the accent shows
   through on the letter alone. No width here - the size is the prop,
   and pinning it in CSS is what made the prop a lie on two pages. */
.mark {
  color: var(--text-primary);
}
</style>
