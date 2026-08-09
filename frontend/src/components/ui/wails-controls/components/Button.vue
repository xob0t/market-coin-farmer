<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
</script>

<template>
  <button
    :class="twMerge('inline-flex cursor-default items-center justify-center select-none outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset', $attrs.class as string)"
  >
    <slot />
  </button>
</template>
