<script setup lang="ts">
import { ref, onMounted, computed, onUnmounted, reactive } from 'vue'
import { ConfigService, YaApiService } from "../../bindings/backend";
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { ScrollArea } from '@/components/ui/scroll-area'
import Coin from '@/components/ui/svg/Coin.vue'
import Wheel from '@/components/ui/svg/Wheel.vue'
import { toast } from 'vue-sonner'
import { Account } from "../../bindings/backend";
import { Clipboard } from "@wailsio/runtime";
import { onKeyStroke } from '@vueuse/core'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'

const config = ref<any>(null)
const cookies = ref('')
const accountsData = reactive<Record<string, {
  rewards: any[],
  signInInfo: any,
  login?: string,
  coinBalance?: string
}>>({})
const name = ref('')
const proxy = ref('')
const editingAccountCookies = ref<string | null>(null)
const editName = ref('')
const editProxy = ref('')
const editCookies = ref('')
const now = ref(Math.floor(Date.now() / 1000))
let countdownInterval: number | null = null

const loadingAccounts = reactive<Record<string, boolean>>({})
const refreshingAll = ref(false)
const hideJunk = ref(false)
const spendingAllCoins = ref(false)

onMounted(async () => {
  await claimAndUpdateAccountInfo()
  startCountdownTimer()
})

onUnmounted(() => {
  if (countdownInterval) {
    clearInterval(countdownInterval)
  }
})

const startCountdownTimer = () => {
  if (countdownInterval) {
    clearInterval(countdownInterval)
  }
  countdownInterval = window.setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
  }, 1000)
}

const formatTime = (seconds: number): string => {
  if (seconds <= 0) return "Доступно сейчас"
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60
  return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`
}

const nextCoinRewardTime = computed(() => {
  const result: Record<string, string> = {}
  if (!config.value?.accounts) return result

  for (const account of config.value.accounts) {
    const accountData = accountsData[account.cookies]
    if (accountData?.signInInfo?.rewardAvailable) {
      claimDailyCoins(account)
      claimDailyGameReward(account)
      getRewards(account)
    }

    if (!accountData?.signInInfo?.nextRewardTs) {
      result[account.cookies] = "Не доступно"
      continue
    }

    const timestamp = accountData.signInInfo.nextRewardTs
    const diff = timestamp - now.value

    result[account.cookies] = formatTime(diff)

    if (diff <= 0) {
      claimDailyCoins(account)
      claimDailyGameReward(account)
      getRewards(account)
    }
  }
  return result
})

const decodeBase64 = (str: string): string => {
  try {
    return atob(str)
  } catch (e) {
    console.error("Ошибка декодирования base64:", e)
    return str
  }
}

const getConfig = async (): Promise<void> => {
  try {
    const resultValue = await ConfigService.GetConfig({})
    config.value = resultValue

    // Initialize account data for new accounts
    if (config.value?.accounts) {
      for (const account of config.value.accounts) {
        if (!accountsData[account.cookies]) {
          accountsData[account.cookies] = {
            rewards: [],
            signInInfo: {}
          }
        }
      }

      // Clean up data for removed accounts
      const currentAccountCookies = config.value.accounts.map((a: Account) => a.cookies)
      Object.keys(accountsData).forEach(cookies => {
        if (!currentAccountCookies.includes(cookies)) {
          delete accountsData[cookies]
        }
      })
    }
  } catch (err) {
    console.error(err)
    toast.error("Ошибка получения конфигурации", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const addAccount = async (): Promise<void> => {
  if (!cookies.value.trim()) {
    toast.error("Требуется cookies")
    return
  }

  try {
    const account = new Account()
    account.cookies = cookies.value.trim()
    account.proxy = proxy.value.trim()
    account.name = name.value.trim() || ''

    await ConfigService.AddAccountToConfig(account)
    toast.success(account.name || accountsData[account.cookies]?.login || '', {
      description: "Аккаунт успешно добавлен",
    })

    // Initialize account data
    accountsData[account.cookies] = {
      rewards: [],
      signInInfo: {}
    }

    await getConfig()

    name.value = ''
    cookies.value = ''
    proxy.value = ''

    // Find the updated account in config
    const newAccount = config.value?.accounts?.find((a: Account) => a.cookies === btoa(account.cookies))
    if (newAccount) {
      console.log(newAccount)
      await claimDailyGameReward(newAccount)
      if (newAccount?.signInInfo?.rewardAvailable) {
        await claimDailyCoins(newAccount)
      }
      await getRewards(newAccount)
    }

  } catch (err) {
    console.error(err)
    toast.error("Ошибка добавления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const removeAccount = async (account: Account): Promise<void> => {
  try {
    await ConfigService.RemoveAccountFromConfig(account)
    delete accountsData[account.cookies]
    delete loadingAccounts[account.cookies]
    toast.success(account.name || accountsData[account.cookies]?.login || '', {
      description: "Аккаунт успешно удален",
    })
    await getConfig()
  } catch (err) {
    console.error(err)
    toast.error("Ошибка удаления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const getRewards = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const [rewardsJson, login, coinBalance] = await YaApiService.GetRewardsJson(account)
    // Create a new object reference to force reactivity
    accountsData[account.cookies] = {
      ...accountsData[account.cookies],
      rewards: JSON.parse(rewardsJson).results?.[0]?.data?.result?.user_rewards || [],
      login,
      coinBalance
    }
  } catch (err) {
    console.error(err)
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения наград " + (err instanceof Error ? err.message : String(err)),
    })
  } finally {
    loadingAccounts[account.cookies] = false
  }
}
const claimAndUpdateAccountInfo = async (): Promise<void> => {
  refreshingAll.value = true
  try {
    await getConfig()
    if (!config.value?.accounts) return

    const promises: Promise<void>[] = []
    for (const account of config.value.accounts) {
      promises.push(claimDailyCoins(account))
      promises.push(claimDailyGameReward(account))
      promises.push(getRewards(account))
    }
    await Promise.all(promises)
  } finally {
    refreshingAll.value = false
  }
}

const claimDailyCoins = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const signInInfo = await YaApiService.ClaimDailyCoins(account)
    const parsedData = JSON.parse(signInInfo)
    // Create a new object reference
    accountsData[account.cookies] = {
      ...accountsData[account.cookies],
      signInInfo: parsedData?.results?.[0]?.data?.result?.info
    }
    if (parsedData.results?.[0]?.data?.result?.shortInfo?.status === "SUCCESS") {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: "Сегодняшние монеты успешно получены",
      })
    }
  } catch (err) {
    console.error(err)
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения награды " + (err instanceof Error ? err.message : String(err)),
    })
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const claimDailyGameReward = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const gameRewardStatus = await YaApiService.ClaimDailyGameReward(account)
    const parsedData = JSON.parse(gameRewardStatus)
    if (parsedData.results?.[0]?.data?.result) {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: "Игровая награда успешно получена",
      })
    }
  } catch (err) {
    console.error(err)
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения награды " + (err instanceof Error ? err.message : String(err)),
    })
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const roll = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const rollStatus = await YaApiService.Roll(account)
    const parsedData = JSON.parse(rollStatus)
    const status = parsedData.results?.[0]?.data?.result?.type
    if (status === "not_enough_coins") {
      toast.error(account.name || accountsData[account.cookies]?.login || '', {
        description: "Не хватает монет!",
      })
      return
    }
    if (status === "success") {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: "Награда из колеса получена!",
      })
      await getRewards(account)
      return
    }
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения награды. Неизвестный статус " + status,
    })
  } catch (err) {
    console.error(err)
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения награды " + (err instanceof Error ? err.message : String(err)),
    })
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const spendAllCoins = async (): Promise<void> => {
  if (!config.value?.accounts) return;

  spendingAllCoins.value = true;
  try {
    await Promise.all(
      config.value.accounts.map(async (account) => {
        while (canRoll(account.cookies)) {
          await roll(account);
        }
      })
    );
    toast.success("Все монеты потрачены на всех аккаунтах");
  } catch (err) {
    console.error(err);
    toast.error("Ошибка при трате монет", {
      description: err instanceof Error ? err.message : String(err),
    });
  } finally {
    spendingAllCoins.value = false;
  }
};

const startEditing = (account: Account): void => {
  editingAccountCookies.value = account.cookies
  editName.value = account.name || ''
  editProxy.value = account.proxy || ''
  editCookies.value = decodeBase64(account.cookies || '')
}

const cancelEditing = (): void => {
  editingAccountCookies.value = null
  editName.value = ''
  editProxy.value = ''
  editCookies.value = ''
}

const saveEditing = async (oldAccount: Account): Promise<void> => {
  if (!editCookies.value.trim()) {
    toast.error("Требуется cookies")
    return
  }

  try {
    await ConfigService.RemoveAccountFromConfig(oldAccount)

    const account = new Account()
    account.cookies = editCookies.value.trim()
    account.proxy = editProxy.value.trim()
    account.name = editName.value.trim() || "" // Keep as undefined if empty

    await ConfigService.AddAccountToConfig(account)

    // Update data reference if cookies changed
    if (oldAccount.cookies !== account.cookies && accountsData[oldAccount.cookies]) {
      accountsData[account.cookies] = accountsData[oldAccount.cookies]
      delete accountsData[oldAccount.cookies]
      delete loadingAccounts[oldAccount.cookies]
    }

    toast.success("Аккаунт успешно обновлен")
    editingAccountCookies.value = null

    // Refresh config to get the properly stored account instance
    await getConfig()

    console.log(account)

    // Find the updated account in config
    const updatedAccount = config.value?.accounts?.find((a: Account) => a.cookies === btoa(account.cookies))
    if (updatedAccount) {
      console.log(updatedAccount)
      await getRewards(updatedAccount)
    }
  } catch (err) {
    console.error(err)
    toast.error("Ошибка обновления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

onKeyStroke('Escape', (e) => {
  if (editingAccountCookies.value) {
    cancelEditing()
    e.preventDefault()
  }
})

const canRoll = (cookies: string): boolean => {
  if (loadingAccounts[cookies]) return false
  const balance = accountsData[cookies]?.coinBalance
  if (!balance) return false
  return parseInt(balance) >= 10
}

const copyPromocode = (promocode: string) => {
  Clipboard.SetText(promocode)
  toast.success(`Промокод ${promocode} скопирован`)
}

const sortedRewards = (rewards: any[]) => {
  if (!rewards) return []
  return [...rewards].sort((a, b) => {
    const aHasPromo = a.actions?.some((act: any) => act.promocode)
    const bHasPromo = b.actions?.some((act: any) => act.promocode)
    return bHasPromo ? 1 : aHasPromo ? -1 : 0
  }).filter(reward => reward.subtitle !== "Больше не действует")
}

const filteredRewards = (rewards: any[]) => {
  if (!rewards) return []
  return rewards.filter(reward => {
    if (hideJunk.value) {
      return !reward.title.startsWith("Скидка")
    }
    return true
  })
}

const getAccountDisplayName = (account: Account): string => {
  const data = accountsData[account.cookies]
  return account.name
    ? `${account.name}${data?.login ? ` (${data.login})` : ''}`
    : data?.login || "Аккаунт"
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="grid grid-cols-1 md:grid-cols-6 gap-2">
      <Input v-model="name" placeholder="Имя (опционально)" title="Имя аккаунта" />
      <Textarea v-model="cookies" placeholder="*Cookies (Netscape)" required class="md:col-span-2 resize-none"
        style="height: 36px; min-height: 36px; padding-top: 10px; line-height: 1;" title="Cookies аккаунта" />
      <Input v-model="proxy" placeholder="proxytype://username:password@server:port" class="md:col-span-2"
        title="Прокси для аккаунта" />
      <Button @click="addAccount" class="cursor-pointer h-9" title="Добавить новый аккаунт">Добавить</Button>
    </div>
  </div>

  <div v-if="config?.accounts.length > 0" class="mt-4 space-y-2">
    <div class="grid grid-cols-[auto_auto_auto_1fr] items-center gap-2 select-none">
      <v-icon @click="claimAndUpdateAccountInfo" name="hi-refresh"
        class="size-5 cursor-pointer stroke-primary spin-reverse-hover"
        title="Обновить все аккаунты и получить монетки" />
      <Button @click="spendAllCoins" :disabled="spendingAllCoins" class="cursor-pointer" title="Потратить все монеты"
        variant="outline">
        Потратить все монеты
        <v-icon v-if="spendingAllCoins" name="hi-refresh" class="size-4 ml-1 animate-spin" />
      </Button>
      <Label for="hideJunk" class="size-full ph-2 cursor-pointer">Скрыть скидки
        <Switch id="hideJunk" v-model="hideJunk" />
      </Label>
      <p v-if="config.accounts.length > 0" class="justify-self-end">
        <span class="text-gray-500">Монетки за вход:</span>
        {{ nextCoinRewardTime[config.accounts[0].cookies] || 'Н/Д' }}
      </p>
    </div>

    <ScrollArea>
      <div v-for="account in config.accounts" :key="account.cookies" class="p-3 border rounded mb-2 relative">
        <div v-if="loadingAccounts[account.cookies] || spendingAllCoins"
          class="absolute inset-0 bg-black/60 flex items-center justify-center z-10">
          <div class="spin-reverse">
            <v-icon name="hi-refresh" class="size-6 stroke-primary" />
          </div>
        </div>

        <div class="flex justify-between items-start gap-2">
          <div v-if="editingAccountCookies !== account.cookies" class="flex-grow space-y-1">
            <div class="flex items-center gap-2">
              <p class="font-medium">
                {{ getAccountDisplayName(account) }}
              </p>
            </div>
            <div v-if="accountsData[account.cookies]?.signInInfo?.plan" class="mt-1">
              <div class="flex gap-1 mt-1">
                <div v-for="(day, dayIndex) in accountsData[account.cookies].signInInfo.plan"
                  :title="`День ${dayIndex + 1}: ${day.reward.amount} монет`" :key="dayIndex"
                  class="w-3 h-3 rounded-full flex items-center justify-center text-xs" :class="{
                    'bg-primary text-white': day.received,
                    'bg-gray-200': !day.received
                  }">
                </div>
              </div>
            </div>

            <div class="flex flex-col gap-1 text-sm">
              <p><span class="text-gray-500 select-none">Прокси: </span>{{ account.proxy || 'Нет' }}</p>
              <p class="flex items-center gap-0.5"><span class="text-gray-500 flex select-none">Баланс:</span>
                {{ accountsData[account.cookies]?.coinBalance || 'Н/Д' }}
                <Coin class="inline ml-1" />
              </p>
            </div>

          </div>

          <div v-else class="flex flex-col gap-2 w-full">
            <Input v-model="editName" placeholder="Имя (опционально)" />
            <Input v-model="editProxy" placeholder="Прокси" />
            <Textarea v-model="editCookies" placeholder="Cookies (Netscape)" required class="h-24" />
            <div class="flex gap-2">
              <Button @click="saveEditing(account)" class="cursor-pointer flex-1" title="Сохранить изменения">
                Сохранить
              </Button>
              <Button @click="cancelEditing()" variant="outline" class="cursor-pointer flex-1"
                title="Отменить редактирование">
                Отмена
              </Button>
            </div>
          </div>

          <div class="flex gap-1 flex-col">
            <div class="flex gap-1 flex-shrink-0 self-end">
              <template v-if="editingAccountCookies !== account.cookies">
                <Button @click="roll(account)"
                  :disabled="!canRoll(account.cookies) || loadingAccounts[account.cookies] || spendingAllCoins"
                  class="cursor-pointer p-0 w-8 h-8" title="Вращать колесо (стоимость 10 монет)">
                  <Wheel class="size-5 fill-black" />
                </Button>
                <Button @click="startEditing(account)" variant="secondary" class="cursor-pointer w-8 h-8"
                  :disabled="loadingAccounts[account.cookies] || spendingAllCoins" title="Редактировать аккаунт">
                  <v-icon name="md-modeeditoutline" class="size-5" />
                </Button>
                <Button @click="removeAccount(account)" variant="destructive" class="cursor-pointer w-8 h-8"
                  :disabled="loadingAccounts[account.cookies] || spendingAllCoins" title="Удалить аккаунт">
                  <v-icon name="bi-trash-fill" class="size-5" />
                </Button>
              </template>

            </div>
            <div v-if="filteredRewards(accountsData[account.cookies]?.rewards)?.length > 0" class="mt-2 self-end">
              <div class="flex flex-wrap gap-1">
                <HoverCard
                  v-for="(reward, rewardIndex) in sortedRewards(filteredRewards(accountsData[account.cookies].rewards))"
                  :key="rewardIndex">
                  <HoverCardTrigger>
                    <div class="relative">
                      <img v-if="reward.reward_image" :src="reward.reward_image" alt="Награда"
                        class="w-15 h-15 object-contain cursor-pointer">
                    </div>
                  </HoverCardTrigger>
                  <HoverCardContent class="w-64">
                    <h4 class="font-medium">{{ reward.title }}</h4>
                    <p v-if="reward.subtitle && reward.subtitle !== 'Больше не действует'"
                      class="text-sm text-gray-600 mt-1">{{
                        reward.subtitle }}</p>
                    <div v-if="reward.actions?.some(a => a.promocode)" class="mt-2">
                      <p class="text-xs font-medium">Промокод:</p>
                      <div class="flex flex-wrap gap-1 mt-1">
                        <Button v-for="(action, actionIndex) in reward.actions.filter(a => a.promocode)"
                          :key="actionIndex" @click="copyPromocode(action.promocode)" size="sm" variant="outline"
                          class="h-6" :title="`Скопировать промокод: ${action.promocode}`">
                          {{ action.text }}
                        </Button>
                      </div>
                    </div>
                  </HoverCardContent>
                </HoverCard>
              </div>
            </div>
          </div>

        </div>
      </div>
    </ScrollArea>
  </div>

  <div v-if="config?.accounts.length === 0" class="flex items-center justify-center w-full h-full flex-col color-red">
    <v-icon name="co-sad" class="size-60 color-fix wrench" />
    <p>
      Аккаунты не найдены.
    </p>
  </div>
</template>