<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AccountManager from './components/AccountManager.vue'
import { Toaster } from '@/components/ui/sonner'
import { VersionService } from "../bindings/backend";
import './index.css'

import { useColorMode } from '@vueuse/core'
import WindowTitlebar from "@/components/ui/wails-controls/WindowTitlebar.vue"

useColorMode().value = "dark"

const version = ref<string>('')

onMounted(async () => {
  try {
    const v = await VersionService.GetVersion()
    version.value = v
  } catch (error) {
    console.error("Failed to get version:", error)
    version.value = 'dev' // Fallback version
  }
})
</script>

<template>
  <div>
    <WindowTitlebar class="absolute top-0 left-0 w-full bg-black">
      <span class="ml-4 font-semibold">Market Coin Farmer <span v-if="version" class="text-muted-foreground">{{ version
          }}</span></span>
    </WindowTitlebar>
  </div>

  <Toaster />
  <main class="h-[calc(100vh-2rem)] w-full bottom-0 mt-8 p-9 overflow-hidden bg-background/92">
    <AccountManager />
  </main>
</template>