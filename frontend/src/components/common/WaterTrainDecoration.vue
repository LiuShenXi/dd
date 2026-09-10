<template>
  <div class="water-train-decoration" :class="`water-train-decoration--${variant}`" aria-hidden="true">
    <img
      :src="waterTrain"
      alt=""
      width="1536"
      height="1024"
      decoding="async"
      draggable="false"
    />
  </div>
</template>

<script setup lang="ts">
import waterTrain from '@/assets/water-train.webp'

withDefaults(defineProps<{
  variant?: 'carpool' | 'login'
}>(), {
  variant: 'carpool'
})
</script>

<style scoped>
.water-train-decoration {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
  user-select: none;
}

.water-train-decoration img {
  --train-distance-mask: linear-gradient(
    160deg,
    transparent 8%,
    rgb(0 0 0 / 12%) 24%,
    rgb(0 0 0 / 55%) 38%,
    #000 52%
  );
  position: absolute;
  right: 0.5rem;
  bottom: -1.125rem;
  width: min(45vw, 90rem, 110vh);
  height: auto;
  aspect-ratio: 3 / 2;
  opacity: 1;
  -webkit-mask-image: var(--train-distance-mask);
  mask-image: var(--train-distance-mask);
}

.water-train-decoration--login img {
  width: min(51vw, 100rem, 115vh);
}

:global(.dark .water-train-decoration img) {
  filter: brightness(1.5) saturate(0.75);
}

@media (max-width: 1023px) {
  .water-train-decoration img {
    width: min(70vw, 48rem);
  }
}

@media (max-width: 639px) {
  .water-train-decoration img {
    right: -1rem;
    bottom: -0.75rem;
    width: min(85vw, 30rem);
  }
}

</style>
