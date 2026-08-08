<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
import Button from '../components/Button.vue'
import Icons from '../components/Icons.vue'
import { closeWindow, isWindowMaximized, maximizeWindow, minimizeWindow } from '../utils/window'
</script>

<template>
  <div :class="twMerge('h-8 flex', $attrs.class as string)" class="mt-[2px] mr-[2px]">
    <!-- Minimize Button -->
    <Button class="group h-8 w-[46px] rounded-none bg-transparent text-foreground/70 transition-all duration-150 hover:bg-accent hover:text-foreground active:bg-accent/80" @click="minimizeWindow">
      <Icons icon="minimizeWin" />
    </Button>

    <!-- Maximize/Restore Button -->
    <Button class="group h-8 w-[46px] rounded-none bg-transparent text-foreground/70 transition-all duration-150 hover:bg-accent hover:text-foreground active:bg-accent/80" @click="maximizeWindow">
      <Icons :icon="isWindowMaximized ? 'maximizeRestoreWin' : 'maximizeWin'" />
    </Button>

    <!-- Close Button -->
    <Button
      class="group h-8 w-[46px] rounded-none bg-transparent text-foreground/70 transition-all duration-150 hover:bg-red-600 hover:text-white active:bg-red-700 active:text-white dark:hover:bg-red-500 dark:active:bg-red-600"
      @click="closeWindow"
    >
      <Icons icon="closeWin" />
    </Button>
  </div>
</template>
