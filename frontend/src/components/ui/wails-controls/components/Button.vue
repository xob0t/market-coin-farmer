<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
</script>

<template>
  <button :class="twMerge('inline-flex cursor-default items-center justify-center select-none outline-none', $attrs.class as string)">
    <slot />
  </button>
</template>
