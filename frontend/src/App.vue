<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AccountManager from './components/AccountManager.vue'
import { Toaster } from '@/components/ui/sonner'
import { VersionService } from '../bindings/backend'
import './index.css'

import { useColorMode } from '@vueuse/core'
import WindowTitlebar from '@/components/ui/wails-controls/WindowTitlebar.vue'

useColorMode().value = 'dark'

const version = ref<string>('')

onMounted(async () => {
  try {
    const v = await VersionService.GetVersion()
    version.value = v
  } catch (error) {
    console.error('Failed to get version:', error)
    version.value = 'dev' // Fallback version
  }
})
</script>

<template>
  <WindowTitlebar class="absolute top-0 left-0 z-50 w-full border-b border-border/60 bg-background/85 backdrop-blur-md">
    <!-- pointer-events-none: inline spans have clientWidth 0, which fails the
         Wails runtime's drag hit-test; let clicks fall through to the titlebar -->
    <span class="ml-4 text-[13px] font-medium tracking-wide pointer-events-none">
      Market Coin Farmer
      <span v-if="version" class="ml-1 font-normal text-muted-foreground">v{{ version }}</span>
    </span>
  </WindowTitlebar>

  <Toaster />
  <main class="flex h-[calc(100vh-2rem)] w-full flex-col mt-8 overflow-hidden bg-background/92 px-6 pt-2 pb-5">
    <AccountManager />
  </main>
</template>
