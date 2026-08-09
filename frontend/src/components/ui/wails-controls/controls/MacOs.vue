<script setup lang="ts">
import { onKeyStroke, useMouseInElement } from '@vueuse/core'
import { twMerge } from 'tailwind-merge'
import { ref } from 'vue'
import Button from '../components/Button.vue'
import Icons from '../components/Icons.vue'
import { closeWindow, fullscreenWindow, maximizeWindow, minimizeWindow } from '../utils/window'

const winBtns = ref(null)
const { isOutside } = useMouseInElement(winBtns)

const isAltKeyPressed = ref(false)
onKeyStroke(
  'Alt',
  () => {
    isAltKeyPressed.value = true
  },
  { eventName: 'keydown' },
)
onKeyStroke(
  'Alt',
  () => {
    isAltKeyPressed.value = false
  },
  { eventName: 'keyup' },
)

// Individual button hover states for more native feel
const closeHovered = ref(false)
const minimizeHovered = ref(false)
const maximizeHovered = ref(false)
</script>

<template>
  <div ref="winBtns" :class="twMerge('flex items-center space-x-2 px-3 h-6', $attrs.class as string)">
    <!-- Close Button -->
    <Button
      :class="
        twMerge(
          'relative flex h-3 w-3 items-center justify-center rounded-full border-[0.5px] transition-all duration-200 ease-out',
          // Light mode colors
          'border-red-500/30 bg-gradient-to-b from-red-400 to-red-500 shadow-sm',
          // Dark mode colors
          'dark:border-red-500/40 dark:from-red-500 dark:to-red-600',
          // Hover states
          closeHovered ? 'scale-105 shadow-md from-red-500 to-red-600 dark:from-red-400 dark:to-red-500' : '',
          // Active states
          'active:scale-95 active:from-red-600 active:to-red-700',
        )
      "
      @click="closeWindow"
      @mouseenter="closeHovered = true"
      @mouseleave="closeHovered = false"
    >
      <Icons
        icon="closeMac"
        :class="
          twMerge(
            'h-2 w-2 text-red-900/80 transition-all duration-200 ease-out',
            'dark:text-red-100/90',
            !isOutside ? 'opacity-100' : 'opacity-0',
            closeHovered ? 'scale-110 text-red-100 dark:text-red-50' : '',
          )
        "
      />
    </Button>

    <!-- Minimize Button -->
    <Button
      :class="
        twMerge(
          'relative flex h-3 w-3 items-center justify-center rounded-full border-[0.5px] transition-all duration-200 ease-out',
          // Light mode colors
          'border-yellow-500/30 bg-gradient-to-b from-yellow-400 to-yellow-500 shadow-sm',
          // Dark mode colors
          'dark:border-yellow-500/40 dark:from-yellow-500 dark:to-yellow-600',
          // Hover states
          minimizeHovered ? 'scale-105 shadow-md from-yellow-500 to-yellow-600 dark:from-yellow-400 dark:to-yellow-500' : '',
          // Active states
          'active:scale-95 active:from-yellow-600 active:to-yellow-700',
        )
      "
      @click="minimizeWindow"
      @mouseenter="minimizeHovered = true"
      @mouseleave="minimizeHovered = false"
    >
      <Icons
        icon="minMac"
        :class="
          twMerge(
            'h-2 w-2 text-yellow-900/80 transition-all duration-200 ease-out',
            'dark:text-yellow-100/90',
            !isOutside ? 'opacity-100' : 'opacity-0',
            minimizeHovered ? 'scale-110 text-yellow-100 dark:text-yellow-50' : '',
          )
        "
      />
    </Button>

    <!-- Maximize/Fullscreen Button -->
    <Button
      :class="
        twMerge(
          'relative flex h-3 w-3 items-center justify-center rounded-full border-[0.5px] transition-all duration-200 ease-out',
          // Light mode colors
          'border-green-500/30 bg-gradient-to-b from-green-400 to-green-500 shadow-sm',
          // Dark mode colors
          'dark:border-green-500/40 dark:from-green-500 dark:to-green-600',
          // Hover states
          maximizeHovered ? 'scale-105 shadow-md from-green-500 to-green-600 dark:from-green-400 dark:to-green-500' : '',
          // Active states
          'active:scale-95 active:from-green-600 active:to-green-700',
        )
      "
      @click="isAltKeyPressed ? maximizeWindow() : fullscreenWindow()"
      @mouseenter="maximizeHovered = true"
      @mouseleave="maximizeHovered = false"
    >
      <template v-if="!isOutside">
        <Icons
          v-if="isAltKeyPressed"
          icon="plusMac"
          :class="twMerge('h-2 w-2 text-green-900/80 transition-all duration-200 ease-out', 'dark:text-green-100/90', maximizeHovered ? 'scale-110 text-green-100 dark:text-green-50' : '')"
        />
        <Icons
          v-else
          icon="fullMac"
          :class="twMerge('h-2 w-2 text-green-900/80 transition-all duration-200 ease-out', 'dark:text-green-100/90', maximizeHovered ? 'scale-110 text-green-100 dark:text-green-50' : '')"
        />
      </template>
    </Button>
  </div>
</template>
