<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
import Button from '../../components/Button.vue'
import Icons from '../../components/Icons.vue'
import { closeWindow, isWindowMaximized, maximizeWindow, minimizeWindow } from '../../utils/window'
</script>

<template>
  <div :class="twMerge('mr-[10px] h-auto items-center space-x-[13px]', $attrs.class as string)">
    <!-- Minimize Button -->
    <Button
      class="group m-0 aspect-square h-6 w-6 cursor-default rounded-full p-0 transition-all duration-200 ease-out bg-[#dadada] text-[#3d3d3d] hover:bg-[#c8c8c8] hover:shadow-sm active:bg-[#b8b8b8] active:scale-95 dark:bg-[#373737] dark:text-white dark:hover:bg-[#484848] dark:active:bg-[#606060]"
      @click="minimizeWindow"
    >
      <Icons icon="minimizeWin" class="h-[9px] w-[9px] transition-transform duration-200 ease-out group-hover:scale-110 group-active:scale-90" />
    </Button>

    <!-- Maximize/Restore Button -->
    <Button
      class="group m-0 aspect-square h-6 w-6 cursor-default rounded-full p-0 transition-all duration-200 ease-out bg-[#dadada] text-[#3d3d3d] hover:bg-[#c8c8c8] hover:shadow-sm active:bg-[#b8b8b8] active:scale-95 dark:bg-[#373737] dark:text-white dark:hover:bg-[#484848] dark:active:bg-[#606060]"
      @click="maximizeWindow"
    >
      <Icons v-if="isWindowMaximized" icon="maximizeRestoreWin" class="h-[9px] w-[9px] transition-transform duration-200 ease-out group-hover:scale-110 group-active:scale-90" />
      <Icons v-else icon="maximizeWin" class="h-2 w-2 transition-transform duration-200 ease-out group-hover:scale-110 group-active:scale-90" />
    </Button>

    <!-- Close Button -->
    <Button
      class="group m-0 aspect-square h-6 w-6 cursor-default rounded-full p-0 transition-all duration-200 ease-out bg-[#dadada] text-[#3d3d3d] hover:bg-background hover:text-white hover:shadow-sm active:scale-95 dark:bg-[#373737] dark:text-white dark:hover:bg-[#e74c3c] dark:hover:text-white"
      @click="closeWindow"
    >
      <Icons icon="closeWin" class="h-2 w-2 transition-transform duration-200 ease-out group-hover:scale-110 group-active:scale-90" />
    </Button>
  </div>
</template>
