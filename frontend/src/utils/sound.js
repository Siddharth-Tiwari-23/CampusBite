// Lightweight Web Audio API notification sound (zero external dependencies)

let audioCtx = null
let userInteracted = false
let lastSoundTime = 0

// Listen for first user interaction to seamlessly initialize/unlock AudioContext
function enableAudioOnInteraction() {
  if (userInteracted) return
  userInteracted = true

  try {
    if (!audioCtx) {
      const AudioContextClass = window.AudioContext || window.webkitAudioContext
      if (AudioContextClass) {
        audioCtx = new AudioContextClass()
      }
    }
    if (audioCtx && audioCtx.state === 'suspended') {
      audioCtx.resume().catch(() => {})
    }
  } catch {
    // Ignore audio initialization error
  }

  if (typeof window !== 'undefined') {
    window.removeEventListener('click', enableAudioOnInteraction)
    window.removeEventListener('keydown', enableAudioOnInteraction)
    window.removeEventListener('touchstart', enableAudioOnInteraction)
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('click', enableAudioOnInteraction, { once: true, passive: true })
  window.addEventListener('keydown', enableAudioOnInteraction, { once: true, passive: true })
  window.addEventListener('touchstart', enableAudioOnInteraction, { once: true, passive: true })
}

/**
 * Plays a short, subtle, professional notification chime (~220ms).
 * Debounced to ensure a single event never plays multiple sounds.
 * Fully exception-safe: failures are silently ignored and never disrupt the UI.
 */
export function playNotificationSound() {
  try {
    if (typeof window === 'undefined') return

    // Debounce duplicate sound triggers within 350ms
    const nowMs = Date.now()
    if (nowMs - lastSoundTime < 350) {
      return
    }
    lastSoundTime = nowMs

    if (!audioCtx) {
      const AudioContextClass = window.AudioContext || window.webkitAudioContext
      if (!AudioContextClass) return
      audioCtx = new AudioContextClass()
    }

    if (audioCtx.state === 'suspended') {
      audioCtx.resume().catch(() => {})
    }

    const t = audioCtx.currentTime
    const osc = audioCtx.createOscillator()
    const gain = audioCtx.createGain()

    osc.type = 'sine'
    // Gentle melodic upward chime: 587.33Hz (D5) -> 880Hz (A5)
    osc.frequency.setValueAtTime(587.33, t)
    osc.frequency.exponentialRampToValueAtTime(880.0, t + 0.08)

    gain.gain.setValueAtTime(0.001, t)
    gain.gain.linearRampToValueAtTime(0.12, t + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.001, t + 0.22)

    osc.connect(gain)
    gain.connect(audioCtx.destination)

    osc.start(t)
    osc.stop(t + 0.23)
  } catch {
    // Silently ignore browser autoplay restrictions or audio errors
  }
}
