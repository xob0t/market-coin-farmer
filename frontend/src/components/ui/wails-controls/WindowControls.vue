<!-- WindowControls.vue -->
<script setup lang="ts">
import { twMerge } from 'tailwind-merge'
import { onMounted, ref } from 'vue'
import Gnome from './controls/linux/Gnome.vue'
import MacOs from './controls/MacOs.vue'
import Windows from './controls/Windows.vue'
import type { WindowControlsProps } from './types'
import { getOsType } from './utils/os'

defineOptions({
  inheritAttrs: false,
})

const props = withDefaults(defineProps<WindowControlsProps>(), {
  justify: false,
  hide: false,
  hideMethod: 'display',
  className: '',
})

// Make platform reactive using ref()
const platform = ref<string>(props.platform || 'windows')

onMounted(async () => {
  // Only detect OS if platform wasn't explicitly provided
  if (!props.platform) {
    try {
      const type = await getOsType()
      switch (type) {
        case 'macos':
          platform.value = 'macos'
          break
        case 'linux':
          platform.value = 'gnome'
          break
        default:
          platform.value = 'windows'
      }
    } catch (error) {
      console.warn('Failed to detect OS, defaulting to Windows controls:', error)
      platform.value = 'windows'
    }
  }
})

const customClass = twMerge('flex items-center', props.className, props.hide && (props.hideMethod === 'display' ? 'hidden' : 'invisible'))

// Prevent double-click event from bubbling up to the titlebar
const handleClick = (event: MouseEvent) => {
  event.stopPropagation()
}

const handleDoubleClick = (event: MouseEvent) => {
  event.stopPropagation()
}
</script>

<template>
  <Windows
    v-if="platform === 'windows'"
    style="--wails-draggable: none"
    :class="twMerge(customClass, props.justify && 'ml-auto')"
    data-window-control
    @click="handleClick"
    @dblclick="handleDoubleClick"
  />

  <MacOs
    v-else-if="platform === 'macos'"
    style="--wails-draggable: none"
    :class="twMerge(customClass, props.justify && 'ml-0')"
    data-window-control
    @click="handleClick"
    @dblclick="handleDoubleClick"
  />

  <Gnome
    v-else-if="platform === 'gnome'"
    style="--wails-draggable: none"
    :class="twMerge(customClass, props.justify && 'ml-auto')"
    data-window-control
    @click="handleClick"
    @dblclick="handleDoubleClick"
  />

  <Windows v-else style="--wails-draggable: none" :class="twMerge(customClass, props.justify && 'ml-auto')" data-window-control @click="handleClick" @dblclick="handleDoubleClick" />
</template>
