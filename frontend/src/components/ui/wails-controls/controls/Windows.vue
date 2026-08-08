<!-- src/components/wails-controls/controls/Windows.vue -->
<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
import Button from '../components/Button.vue'
import Icons from '../components/Icons.vue'
import { closeWindow, isWindowMaximized, maximizeWindow, minimizeWindow } from '../utils/window'
</script>

<template>
  <div :class="twMerge('h-8', $attrs.class as string)">
    <Button
      class="max-h-8 w-[46px] cursor-default rounded-none bg-transparent text-black/90 hover:bg-black/[.05] active:bg-black/[.03] dark:text-white dark:hover:bg-white/[.06] dark:active:bg-white/[.04]"
      @click="minimizeWindow"
    >
      <Icons icon="minimizeWin" />
    </Button>
    <Button
      :class="
        twMerge(
          'max-h-8 w-[46px] cursor-default rounded-none bg-transparent',
          'text-black/90 hover:bg-black/[.05] active:bg-black/[.03] dark:text-white dark:hover:bg-white/[.06] dark:active:bg-white/[.04]',
        )
      "
      @click="maximizeWindow"
    >
      <Icons v-if="isWindowMaximized" icon="maximizeRestoreWin" />
      <Icons v-else icon="maximizeWin" />
    </Button>
    <Button class="max-h-8 w-[46px] cursor-default rounded-none bg-transparent text-black/90 hover:bg-[#c42b1c] hover:text-white active:bg-[#c42b1c]/90 dark:text-white" @click="closeWindow">
      <Icons icon="closeWin" />
    </Button>
  </div>
</template>
