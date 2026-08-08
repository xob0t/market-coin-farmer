// utils/os.ts
import { System } from '@wailsio/runtime'

export type OsType = 'windows' | 'linux' | 'macos'

let osType: OsType | undefined = undefined
let osTypePromise: Promise<OsType> | null = null

// Synchronous OS detection as fallback
function detectOsSync(): OsType {
  try {
    if (System.IsWindows()) {
      return 'windows'
    } else if (System.IsLinux()) {
      return 'linux'
    } else if (System.IsMac()) {
      return 'macos'
    }
  } catch (error) {
    console.warn('Synchronous OS detection failed:', error)
  }

  // Ultimate fallback - try to detect from user agent
  if (typeof navigator !== 'undefined') {
    const userAgent = navigator.userAgent.toLowerCase()
    if (userAgent.includes('mac')) return 'macos'
    if (userAgent.includes('linux')) return 'linux'
    if (userAgent.includes('win')) return 'windows'
  }

  return 'windows' // Default fallback
}

// Asynchronous OS detection using Environment API
async function detectOsAsync(): Promise<OsType> {
  try {
    const env = await System.Environment()
    const osName = env.OS.toLowerCase()

    switch (osName) {
      case 'windows':
        return 'windows'
      case 'linux':
        return 'linux'
      case 'darwin':
      case 'macos':
        return 'macos'
      default:
        console.warn(`Unknown OS from Environment API: ${osName}, falling back to sync detection`)
        return detectOsSync()
    }
  } catch (error) {
    console.warn('Environment API failed, falling back to sync detection:', error)
    return detectOsSync()
  }
}

// Initialize OS detection if in browser environment
if (typeof window !== 'undefined') {
  osTypePromise = detectOsAsync().then((detectedType) => {
    osType = detectedType
    return detectedType
  })
}

// Main function to get OS type
export function getOsType(): Promise<OsType> {
  // If we already have the OS type cached, return it immediately
  if (osType) {
    return Promise.resolve(osType)
  }

  // If we're not in a browser environment, use sync detection
  if (typeof window === 'undefined') {
    const detectedType = detectOsSync()
    osType = detectedType
    return Promise.resolve(detectedType)
  }

  // If we have a promise in progress, wait for it
  if (osTypePromise) {
    return osTypePromise
  }

  // Create new detection promise
  osTypePromise = detectOsAsync().then((detectedType) => {
    osType = detectedType
    return detectedType
  })

  return osTypePromise
}

// Synchronous getter for cases where you need immediate result
export function getOsTypeSync(): OsType {
  if (osType) {
    return osType
  }

  const detectedType = detectOsSync()
  osType = detectedType
  return detectedType
}
