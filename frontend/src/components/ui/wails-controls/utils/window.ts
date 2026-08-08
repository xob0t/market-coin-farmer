// Alternative TypeScript-safe version if you prefer explicit typing

import { Window, Events } from '@wailsio/runtime'
import { ref } from 'vue'

export const isWindowMaximized = ref(false)

// Type-safe timeout handling
let updateTimeout: number | undefined = undefined

// Initialize the window reference and set up event listeners
export const initialize = async () => {
  // Set up window resize event listener to track maximize state
  const updateMaximizedState = async () => {
    try {
      const isMaximized = await Window.IsMaximised()
      if (isMaximized !== undefined) {
        isWindowMaximized.value = isMaximized
      }
    } catch (error) {
      console.warn('Failed to get window maximized state:', error)
    }
  }

  // Debounced version to prevent rapid state updates
  const debouncedUpdateMaximizedState = () => {
    if (updateTimeout !== undefined) {
      window.clearTimeout(updateTimeout)
    }
    updateTimeout = window.setTimeout(updateMaximizedState, 50)
  }

  // Listen for window events using Wails3 events
  Events.On('window:resize', debouncedUpdateMaximizedState)
  Events.On('window:maximize', () => {
    isWindowMaximized.value = true
  })
  Events.On('window:unmaximize', () => {
    isWindowMaximized.value = false
  })
  Events.On('window:restore', debouncedUpdateMaximizedState)

  // Initial state check with retry logic
  const initializeMaximizedState = async () => {
    let retries = 3
    while (retries > 0) {
      try {
        await updateMaximizedState()
        break
      } catch (error) {
        retries--
        if (retries === 0) {
          console.warn('Failed to initialize window maximized state after retries:', error)
        } else {
          await new Promise<void>((resolve) => window.setTimeout(resolve, 100))
        }
      }
    }
  }

  // Initial state check
  initializeMaximizedState()
}

export const minimizeWindow = async () => {
  try {
    await Window.Minimise()
  } catch (error) {
    console.error('Failed to minimize window:', error)
  }
}

export const maximizeWindow = async () => {
  try {
    // Store the current state before toggling
    const wasMaximized = isWindowMaximized.value

    await Window.ToggleMaximise()

    // Update state immediately with expected value
    isWindowMaximized.value = !wasMaximized

    // Verify the actual state after a short delay
    window.setTimeout(async () => {
      try {
        const actualState = await Window.IsMaximised()
        if (actualState !== undefined && actualState !== isWindowMaximized.value) {
          isWindowMaximized.value = actualState
        }
      } catch (error) {
        console.warn('Failed to verify maximized state after toggle:', error)
      }
    }, 100)
  } catch (error) {
    console.error('Failed to toggle maximize window:', error)
  }
}

// Alternative explicit maximize/unmaximize functions for better reliability
export const maximizeWindowExplicit = async () => {
  try {
    await Window.Maximise()
    isWindowMaximized.value = true

    // Verify after a short delay
    window.setTimeout(async () => {
      try {
        const actualState = await Window.IsMaximised()
        if (actualState !== undefined) {
          isWindowMaximized.value = actualState
        }
      } catch (error) {
        console.warn('Failed to verify maximized state:', error)
      }
    }, 100)
  } catch (error) {
    console.error('Failed to maximize window:', error)
  }
}

export const unmaximizeWindow = async () => {
  try {
    await Window.UnMaximise()
    isWindowMaximized.value = false

    // Verify after a short delay
    window.setTimeout(async () => {
      try {
        const actualState = await Window.IsMaximised()
        if (actualState !== undefined) {
          isWindowMaximized.value = actualState
        }
      } catch (error) {
        console.warn('Failed to verify maximized state:', error)
      }
    }, 100)
  } catch (error) {
    console.error('Failed to unmaximize window:', error)
  }
}

export const restoreWindow = async () => {
  try {
    await Window.Restore()

    // Update state after restore
    window.setTimeout(async () => {
      try {
        const actualState = await Window.IsMaximised()
        if (actualState !== undefined) {
          isWindowMaximized.value = actualState
        }
      } catch (error) {
        console.warn('Failed to update state after restore:', error)
      }
    }, 100)
  } catch (error) {
    console.error('Failed to restore window:', error)
  }
}

export const fullscreenWindow = async () => {
  try {
    if (Window) {
      const isFullscreen = await Window.IsFullscreen()
      if (isFullscreen) {
        await Window.UnFullscreen()
      } else {
        await Window.Fullscreen()
      }
    }
  } catch (error) {
    console.error('Failed to toggle fullscreen:', error)
  }
}

export const closeWindow = async () => {
  try {
    await Window.Close()
  } catch (error) {
    console.error('Failed to close window:', error)
  }
}
