<!-- WindowTitlebar.vue -->
<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
import { onMounted, ref, computed } from 'vue'
import type { WindowControlsProps, WindowTitlebarProps } from './types'
import type { OsType } from './utils/os'
import { getOsType } from './utils/os'
import { maximizeWindow, initialize } from './utils/window'
import WindowControls from './WindowControls.vue'

const { controlsOrder = 'system', windowControlsProps } = defineProps<WindowTitlebarProps>()

const osType = ref<OsType>('windows') // Default to windows

onMounted(async () => {
  initialize()
  try {
    const detectedType = await getOsType()
    osType.value = detectedType
  } catch (error) {
    console.warn('Failed to detect OS in titlebar, defaulting to Windows:', error)
    osType.value = 'windows'
  }
})

// Use computed property for reactive left positioning
const left = computed(() => {
  return controlsOrder === 'left' || (controlsOrder === 'platform' && windowControlsProps?.platform === 'macos') || (controlsOrder === 'system' && osType.value === 'macos')
})

const customProps = (ml: string) => {
  if (windowControlsProps?.justify !== undefined) return windowControlsProps

  const { justify: windowControlsJustify, className: windowControlsClassName, ...restProps } = windowControlsProps || {}
  return {
    justify: false,
    className: twMerge(windowControlsClassName, ml),
    ...restProps,
  } as WindowControlsProps
}

// Double-click to maximize functionality
const handleDoubleClick = (event: MouseEvent) => {
  // Only handle double-click on the draggable area, not on controls
  const target = event.target as HTMLElement

  // Check if the click was on a control button or its children
  if (target.closest('[data-window-control]')) {
    return
  }

  // Trigger maximize window on double-click
  maximizeWindow()
}
</script>

<template>
  <div
    :class="
      twMerge(
        'relative flex select-none flex-row items-center overflow-hidden bg-background/95 backdrop-blur-sm ',
        'h-8', // Standard titlebar height
        $attrs.class as string,
      )
    "
    style="--wails-draggable: drag"
    @dblclick="handleDoubleClick"
  >
    <template v-if="left">
      <!-- macOS: Controls on left, title left-aligned after them -->
      <WindowControls v-bind="customProps('ml-0')" data-window-control />
      <div class="flex-1 flex items-center px-4 text-sm font-medium text-foreground/90 truncate">
        <slot />
      </div>
    </template>
    <template v-else>
      <!-- Windows/Linux: title left-aligned, controls on right -->
      <div class="flex-1 flex items-center px-4 text-sm font-medium text-foreground/90 truncate">
        <slot />
      </div>
      <WindowControls v-bind="customProps('ml-auto')" data-window-control />
    </template>
  </div>
</template>
