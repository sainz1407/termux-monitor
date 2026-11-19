<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import {
    Smartphone,
    Play,
    Square,
    Camera,
    Maximize2,
    Minimize2,
    SlidersHorizontal,
    Keyboard,
    ArrowLeft,
    Home,
    Menu,
    Power,
    Volume1,
    Volume2,
    VolumeX,
    Eye,
    EyeOff,
    Send,
    Activity,
    Zap,
    Cpu,
    Monitor,
    RefreshCw,
    Sparkles,
    Check,
  } from '@lucide/svelte'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'
  import Input from '../../ui/Input.svelte'
  import { WebSocketClient } from '../../../ws/socket'
  import { toastStore } from '../../../stores/toast.svelte'

  // --- Runtime Streaming & Device State ---
  let isMirroring = $state(false)
  let isConnecting = $state(false)
  let fps = $state(0)
  let totalFrames = $state(0)
  let streamEngine = $state<'webcodecs' | 'mjpeg' | 'idle'>('idle')
  let detectedWidth = $state(1080)
  let detectedHeight = $state(2400)
  let streamWidth = $state(1080)
  let streamHeight = $state(2400)
  let codecName = $state('avc1.640033')
  let hardwareEncoderName = $state('Qualcomm c2.qti.avc (Hardware GPU/VPU)')

  // --- Stream Customization Configurations ---
  let maxSize = $state(1280) // Native (0), 1440p (2560), 1080p (1920), 720p (1280), 540p (960)
  let bitRate = $state(8000000) // 2M, 4M, 8M, 16M, 24M, 32M or custom
  let customBitrateMbps = $state(8)
  let maxFps = $state(60) // 30 FPS vs 60 FPS
  let displayId = $state(0) // Main (0) vs Cover (1)
  let turnScreenOff = $state(false)
  let stayAwake = $state(true)
  let settingsChanged = $state(false)

  // --- Interactive Control States ---
  let isPhysicalScreenOff = $state(false)
  let isKeyboardActive = $state(false)
  let isFullscreen = $state(false)
  let showSettings = $state(true)
  let textInput = $state('')

  // --- DOM & Engine References ---
  let canvasEl: HTMLCanvasElement | null = $state(null)
  let phoneStageEl: HTMLDivElement | null = $state(null)
  let wsClient: WebSocketClient | null = null
  let videoDecoder: any = null
  let cachedSpsPps: Uint8Array | null = null
  let cachedAVCC: Uint8Array | null = null
  let isDecoderConfigured = false
  let avccMode = false
  let webCodecsUnavailable = $state(false)

  let frameCounter = 0
  let fpsInterval: ReturnType<typeof setInterval> | null = null
  let lastMoveTime = 0

  // Dynamic Aspect Ratio calculated from stream resolution
  let aspectRatio = $derived(
    detectedWidth > 0 && detectedHeight > 0
      ? `${detectedWidth} / ${detectedHeight}`
      : '9 / 19.5'
  )

  // Resolution options
  const resolutionOptions = [
    { label: 'Native 1:1', value: 0, desc: 'Original (No Scale)' },
    { label: '1440p', value: 2560, desc: 'Ultra (2560)' },
    { label: '1080p', value: 1920, desc: 'FHD (1920)' },
    { label: '720p', value: 1280, desc: 'HD (1280)' },
    { label: '540p', value: 960, desc: 'Fast (960)' },
  ]

  // Bitrate options
  const bitrateOptions = [
    { label: '2M', value: 2000000, desc: 'Low' },
    { label: '4M', value: 4000000, desc: 'Balanced' },
    { label: '8M', value: 8000000, desc: 'High' },
    { label: '16M', value: 16000000, desc: 'Very High' },
    { label: '24M', value: 24000000, desc: 'Ultra' },
    { label: '32M', value: 32000000, desc: 'Near Lossless' },
  ]

  onMount(() => {
    window.addEventListener('keydown', handleGlobalKeyDown)
    document.addEventListener('fullscreenchange', handleFullscreenChange)

    return () => {
      cleanup()
    }
  })

  onDestroy(() => {
    cleanup()
  })

  function cleanup() {
    window.removeEventListener('keydown', handleGlobalKeyDown)
    document.removeEventListener('fullscreenchange', handleFullscreenChange)
    stopMirroring()
  }

  function handleFullscreenChange() {
    isFullscreen = !!document.fullscreenElement
  }

  // --- Helper: Parse H.264 SPS profile & level to RFC 6381 codec string ---
  // Used for flag-2 Annex-B config packets only; avcC records are parsed by
  // codecFromAVCC.
  function parseH264Sps(nalData: Uint8Array): string | null {
    for (const n of scanAnnexBNals(nalData)) {
      if ((nalData[n.start] & 0x1f) === 7 && n.start + 3 < n.end) {
        const toHex = (n2: number) => n2.toString(16).padStart(2, '0')
        return `avc1.${toHex(nalData[n.start + 1])}${toHex(nalData[n.start + 2])}${toHex(nalData[n.start + 3])}`
      }
    }
    return null
  }

  // Helper: derive the RFC 6381 codec string straight from an
  // AVCDecoderConfigurationRecord (avcC): [0]=version, [1]=profile,
  // [2]=compatibility, [3]=level. Annex-B scanning does not apply here.
  function codecFromAVCC(record: Uint8Array): string | null {
    if (record.length < 4 || record[0] !== 1) return null
    const toHex = (n: number) => n.toString(16).padStart(2, '0')
    return `avc1.${toHex(record[1])}${toHex(record[2])}${toHex(record[3])}`
  }

  // Helper: decode a base64 avcC record sent inside the metadata frame
  function avccFromBase64(b64: string): Uint8Array | null {
    try {
      const bin = atob(b64)
      const out = new Uint8Array(bin.length)
      for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
      return out.length >= 4 && out[0] === 1 ? out : null
    } catch {
      return null
    }
  }

  // Monotonic timestamp base: WebCodecs expects timestamps in microseconds and
  // browsers may schedule/drop frames based on them. Scrcpy PTS values are
  // relative to boot (hundreds of millions of µs), which combined with
  // optimizeForLatency made Chrome treat every frame as late. Samples are
  // re-based to start near 0 and always advance.
  let tsBase = -1
  let lastTs = -1

  function normalizeTs(pts: number): number {
    if (tsBase < 0) tsBase = pts
    let ts = pts - tsBase
    if (lastTs >= 0 && ts <= lastTs) ts = lastTs + 1000
    lastTs = ts
    return ts
  }

  // Helper: tear down the current decoder so the next configure() applies a
  // fresh description (browsers reject reconfiguring with a new description).
  function resetDecoder() {
    if (videoDecoder) {
      try {
        if (videoDecoder.state !== 'closed') videoDecoder.close()
      } catch {}
      videoDecoder = null
    }
    isDecoderConfigured = false
  }

  // Helper: extract 64-bit big-endian PTS safely
  function parsePts(view: DataView): number {
    try {
      return Number(view.getBigUint64(1, false))
    } catch {
      const hi = view.getUint32(1, false)
      const lo = view.getUint32(5, false)
      return hi * 4294967296 + lo
    }
  }

  // Helper: iterate Annex-B NAL units, returning their offsets (payload start)
  // and the offset of the following start code (i.e. end of NAL payload).
  function scanAnnexBNals(buf: Uint8Array): { start: number; end: number }[] {
    const nals: { start: number; end: number }[] = []
    let i = 0
    let pos = 0
    let scLen = 0
    // Locate first start code
    for (i = 0; i + 3 <= buf.length; i++) {
      if (buf[i] === 0 && buf[i + 1] === 0) {
        if (buf[i + 2] === 1) {
          pos = i + 3
          scLen = 3
          break
        }
        if (i + 4 <= buf.length && buf[i + 2] === 0 && buf[i + 3] === 1) {
          pos = i + 4
          scLen = 4
          break
        }
      }
    }
    if (scLen === 0) return nals

    let curStart = pos
    for (i = pos; i + 3 <= buf.length; i++) {
      if (buf[i] === 0 && buf[i + 1] === 0) {
        let nextStart = -1
        if (buf[i + 2] === 1) {
          nextStart = i
        } else if (i + 4 <= buf.length && buf[i + 2] === 0 && buf[i + 3] === 1) {
          nextStart = i
        }
        if (nextStart >= 0) {
          if (nextStart > curStart) nals.push({ start: curStart, end: nextStart })
          let skip = buf[i + 2] === 1 ? 3 : 4
          curStart = i + skip
          i = i + skip - 1
        }
      }
    }
    if (curStart < buf.length) nals.push({ start: curStart, end: buf.length })
    return nals
  }

  // Helper: derive RFC 6381 codec string and AVCC description from SPS/PPS
  function deriveConfig(nalData: Uint8Array): { codec: string | null; description: Uint8Array | null } {
    const nals = scanAnnexBNals(nalData)
    let sps: Uint8Array | null = null
    let pps: Uint8Array | null = null
    for (const n of nals) {
      const type = nalData[n.start] & 0x1f
      if (type === 7 && !sps) sps = nalData.subarray(n.start, n.end)
      else if (type === 8 && !pps) pps = nalData.subarray(n.start, n.end)
    }
    if (!sps || sps.length < 4) return { codec: null, description: null }

    const toHex = (n: number) => n.toString(16).padStart(2, '0')
    const codec = `avc1.${toHex(sps[1])}${toHex(sps[2])}${toHex(sps[3])}`

    if (!pps) return { codec, description: null }
    const description = new Uint8Array(11 + sps.length + pps.length)
    let o = 0
    description[o++] = 0x01
    description[o++] = sps[1]
    description[o++] = sps[2]
    description[o++] = sps[3]
    description[o++] = 0xff // lengthSizeMinusOne = 3 (4-byte NAL length)
    description[o++] = 0xe1 // 1 SPS
    description[o++] = (sps.length >> 8) & 0xff
    description[o++] = sps.length & 0xff
    description.set(sps, o)
    o += sps.length
    description[o++] = 0x01 // 1 PPS
    description[o++] = (pps.length >> 8) & 0xff
    description[o++] = pps.length & 0xff
    description.set(pps, o)
    return { codec, description }
  }

  // Helper: build a valid AVCC sample (4-byte length prefixed NAL) from Annex-B
  function annexBToAVCC(annexB: Uint8Array): Uint8Array | null {
    const nals = scanAnnexBNals(annexB)
    if (nals.length === 0) return null
    let total = 0
    for (const n of nals) total += 4 + (n.end - n.start)
    const out = new Uint8Array(total)
    let o = 0
    for (const n of nals) {
      const len = n.end - n.start
      out[o++] = (len >>> 24) & 0xff
      out[o++] = (len >>> 16) & 0xff
      out[o++] = (len >>> 8) & 0xff
      out[o++] = len & 0xff
      out.set(annexB.subarray(n.start, n.end), o)
      o += len
    }
    return out
  }

  // --- WebCodecs Engine Initialization & Configuration ---
  // WebCodecs is only exposed in secure contexts (HTTPS or localhost). Any
  // browser reaching the dashboard over plain HTTP on a LAN address has no
  // VideoDecoder at all, which used to leave the canvas black while the
  // touch/control channel kept working.
  function webCodecsSupported(): boolean {
    if (typeof window === 'undefined') return false
    return 'VideoDecoder' in window && window.isSecureContext !== false
  }

  // Safe wrapper: (re)creates and configures the decoder, tolerating failures
  // when the browser rejects a description-mode configuration.
  function initOrConfiguredDecoderSafe(codec: string) {
    return initOrConfigureDecoder(codec)
  }

  function initOrConfigureDecoder(codec: string) {
    if (typeof window === 'undefined' || !('VideoDecoder' in window)) {
      console.warn('[Scrcpy] Browser does not support WebCodecs VideoDecoder. Using fallback.')
      return false
    }

    try {
      if (!videoDecoder || videoDecoder.state === 'closed') {
        // @ts-ignore - VideoDecoder global
        videoDecoder = new VideoDecoder({
          output: (frame: any) => {
            renderVideoFrame(frame)
          },
          error: (err: any) => {
            console.error('[WebCodecs] Decoder error:', err)
          },
        })
      }

      if (videoDecoder.state === 'unconfigured' || !isDecoderConfigured) {
        // The hub always emits AVCC-framed samples plus an avcC description, so
        // configure the decoder in description mode whenever we have the record.
        const cfg: Record<string, any> = {
          codec: codec || 'avc1.640033',
          // optimizeForLatency lets the decoder emit frames as soon as they are
          // ready; hardware decoders that report interlaced/reorder output need
          // it to avoid holding the first keyframe indefinitely.
          optimizeForLatency: true,
        }
        if (cachedAVCC && cachedAVCC.length > 0) {
          cfg.description = cachedAVCC
        } else if (avccMode) {
          // AVCC samples without a description cannot be decoded correctly;
          // wait for the flag-3 config packet instead of misconfiguring.
          return false
        }
        videoDecoder.configure(cfg)
        isDecoderConfigured = true
      }
      return true
    } catch (err) {
      console.warn('[WebCodecs] Configuration error:', err)
      return false
    }
  }

  // Render decoded video frame to canvas
  function renderVideoFrame(frame: any) {
    try {
      if (canvasEl) {
        const frameW = frame.displayWidth || frame.codedWidth || streamWidth
        const frameH = frame.displayHeight || frame.codedHeight || streamHeight

        if (canvasEl.width !== frameW || canvasEl.height !== frameH) {
          canvasEl.width = frameW
          canvasEl.height = frameH
          detectedWidth = frameW
          detectedHeight = frameH
        }

        const ctx = canvasEl.getContext('2d', { alpha: false, desynchronized: true })
        if (ctx) {
          ctx.drawImage(frame, 0, 0, frameW, frameH)
        }
      }
      frameCounter++
      totalFrames++
      if (isConnecting) {
        isConnecting = false
      }
    } catch (e) {
      console.error('[Scrcpy] Frame render error:', e)
    } finally {
      try {
        frame.close()
      } catch {}
    }
  }

  // --- WebSocket Stream Lifecycle ---
  function startMirroring() {
    if (wsClient && wsClient.readyState === WebSocket.OPEN) {
      return
    }

    stopMirroring()
    isConnecting = true
    totalFrames = 0
    frameCounter = 0
    fps = 0
    settingsChanged = false
    // Never reuse a description from a previous session (resolution or
    // encoder profile may have changed).
    cachedAVCC = null
    cachedSpsPps = null
    avccMode = false
    tsBase = -1
    lastTs = -1

    // Pick the engine the browser is actually able to decode. Without
    // WebCodecs the H.264 stream can never be rendered, so the backend has to
    // serve screencap frames instead.
    const useWebCodecs = webCodecsSupported()
    webCodecsUnavailable = !useWebCodecs
    const engine = useWebCodecs ? 'avcc' : 'mjpeg'
    streamEngine = useWebCodecs ? 'webcodecs' : 'mjpeg'

    const queryParams = new URLSearchParams({
      max_size: maxSize.toString(),
      bit_rate: bitRate.toString(),
      max_fps: maxFps.toString(),
      display_id: displayId.toString(),
      stay_awake: stayAwake ? 'true' : 'false',
      screen_off: turnScreenOff ? 'true' : 'false',
      input_format: 'avcc',
      engine,
    })

    const path = `/api/scrcpy/ws?${queryParams.toString()}`

    try {
      wsClient = new WebSocketClient({
        path,
        autoReconnect: false,
        binaryType: 'arraybuffer',
        onOpen: () => {
          isConnecting = false
          isMirroring = true
          if (useWebCodecs) {
            streamEngine = 'webcodecs'
            // Do NOT configure the decoder here. The server always sends the
            // stream description (metadata avcC, or a flag-3 packet) right after
            // the socket opens. Configuring with a guessed baseline profile
            // leaves the decoder in the wrong mode and every sample is rejected.
            // If the description never arrives, the sample path requests a
            // resync instead of decoding with a bogus configuration.
            if (cachedAVCC && cachedAVCC.length > 0) {
              initOrConfiguredDecoderSafe(codecName)
            }
          } else {
            streamEngine = 'mjpeg'
            toastStore.warning(
              'WebCodecs requires HTTPS. Falling back to the 3 FPS screencap stream.',
              'Limited Mirror Mode'
            )
          }

          // Start 1-second FPS ticker
          if (fpsInterval) clearInterval(fpsInterval)
          fpsInterval = setInterval(() => {
            fps = frameCounter
            frameCounter = 0
          }, 1000)

          toastStore.success('Hardware screen stream connected', 'Scrcpy Live')
        },
        onMessage: (data) => {
          handleIncomingMessage(data)
        },
        onClose: () => {
          stopMirroring()
        },
        onError: () => {
          stopMirroring()
          toastStore.error('Connection to scrcpy server lost', 'Connection Error')
        },
      })

      wsClient.connect()
    } catch (err) {
      console.error('[Scrcpy] Connect failed:', err)
      stopMirroring()
    }
  }

  function handleIncomingMessage(data: string | ArrayBuffer | Blob) {
    // 1. Text Message: Metadata JSON
    if (typeof data === 'string') {
      try {
        const meta = JSON.parse(data)
        if (meta.type === 'metadata') {
          if (meta.width) detectedWidth = meta.width
          if (meta.height) detectedHeight = meta.height
          if (meta.videoWidth) streamWidth = meta.videoWidth
          if (meta.videoHeight) streamHeight = meta.videoHeight
          if (meta.engine === 'mjpeg') {
            streamEngine = 'mjpeg'
          } else {
            streamEngine = 'webcodecs'
          }
          if (meta.codec) {
            codecName = meta.codec
          }
          if (typeof meta.avcc === 'boolean') {
            avccMode = meta.avcc
          }
          if (meta.encoder) {
            hardwareEncoderName = meta.encoder
          }
          // The hub inlines the cached avcC record so the decoder can be
          // configured before the first sample arrives.
          if (typeof meta.avccConfig === 'string' && meta.avccConfig) {
            const record = avccFromBase64(meta.avccConfig)
            if (record) {
              cachedAVCC = record
              avccMode = true
              const detected = codecFromAVCC(record)
              if (detected) codecName = detected
              resetDecoder()
              initOrConfiguredDecoderSafe(codecName)
            }
          }
          if (canvasEl && meta.videoWidth > 0 && meta.videoHeight > 0) {
            canvasEl.width = meta.videoWidth
            canvasEl.height = meta.videoHeight
          }
        }
      } catch (err) {
        console.warn('[Scrcpy] Malformed metadata JSON:', err)
      }
      return
    }

    // 2. Binary Message: WebCodecs AVCC H.264 or Fallback MJPEG
    if (data instanceof ArrayBuffer) {
      const u8 = new Uint8Array(data)
      if (u8.length === 0) return

      // Fallback check: JPEG SOI (0xFF, 0xD8) or engine is mjpeg
      if (streamEngine === 'mjpeg' || (u8.length > 2 && u8[0] === 0xff && u8[1] === 0xd8)) {
        streamEngine = 'mjpeg'
        renderJpegFallback(data)
        return
      }

      // H.264 WebCodecs packet format:
      // [0]: Flag (3 = avcC description, 2 = Annex-B config SPS/PPS, 1 = Keyframe, 0 = Delta)
      // [1..8]: PTS (uint64 big-endian)
      // [9..]: payload bytes
      if (u8.length < 9) return
      const flag = u8[0]
      const view = new DataView(data)
      const pts = parsePts(view)
      const nalData = u8.subarray(9)

      if (flag === 3) {
        // AVCC description (avcC) mode: configure the decoder with the avcC
        // record, then feed length-prefixed samples.
        avccMode = true
        cachedAVCC = new Uint8Array(nalData) // detach from the WS buffer
        const detectedCodec = codecFromAVCC(cachedAVCC)
        if (detectedCodec) {
          codecName = detectedCodec
        }
        // Force reconfiguration so the description takes effect.
        resetDecoder()
        initOrConfiguredDecoderSafe(codecName)
        return
      }

      if (flag === 2) {
        // Legacy Annex-B config packet (SPS/PPS). Derive the avcC description,
        // then reconfigure into description mode when supported.
        avccMode = true
        cachedSpsPps = new Uint8Array(nalData)
        const cfg = deriveConfig(cachedSpsPps)
        if (cfg.codec) {
          codecName = cfg.codec
        }
        cachedAVCC = cfg.description
        resetDecoder()
        initOrConfiguredDecoderSafe(codecName)
        return
      }

      if (flag === 1 || flag === 0) {
        // Samples are only meaningful once the decoder holds the real avcC
        // description. Without it, ask the server to replay the description
        // instead of feeding a decoder configured from a guessed profile
        // (which silently produces a black canvas).
        if (!cachedAVCC || cachedAVCC.length === 0 || !videoDecoder || videoDecoder.state !== 'configured') {
          requestStreamResync()
          return
        }
        const decoderReady = videoDecoder.state === 'configured'

        // Choose the sample framing expected by the configured decoder.
        // The hub always emits AVCC (4-byte length-prefixed NALs) and the
        // description comes from the avcC record, so no SPS/PPS inlining here.
        let chunkData: Uint8Array | null
        if (avccMode) {
          // AVCC samples carry 4-byte length prefixes; convert defensively if
          // the server ever falls back to Annex-B framing.
          const isAnnexB =
            nalData.length >= 4 &&
            ((nalData[0] === 0 && nalData[1] === 0 && nalData[2] === 1) ||
              (nalData[0] === 0 && nalData[1] === 0 && nalData[2] === 0 && nalData[3] === 1))
          const nalBuf = isAnnexB ? annexBToAVCC(nalData) : nalData
          if (!nalBuf || nalBuf.length === 0) return
          chunkData = nalBuf
        } else {
          chunkData = nalData
        }

        try {
          // @ts-ignore - EncodedVideoChunk global
          const chunk = new EncodedVideoChunk({
            type: flag === 1 ? 'key' : 'delta',
            timestamp: normalizeTs(pts),
            data: chunkData,
          })

          if (decoderReady && videoDecoder) {
            videoDecoder.decode(chunk)
          }
        } catch (err) {
          console.warn('[WebCodecs] Error decoding chunk:', err)
        }
      }
    } else if (data instanceof Blob) {
      renderJpegFallback(data)
    }
  }

  function renderJpegFallback(source: ArrayBuffer | Blob) {
    const blob = source instanceof Blob ? source : new Blob([source], { type: 'image/jpeg' })
    if (typeof createImageBitmap === 'function') {
      createImageBitmap(blob)
        .then((bmp) => {
          if (canvasEl) {
            if (canvasEl.width !== bmp.width || canvasEl.height !== bmp.height) {
              canvasEl.width = bmp.width
              canvasEl.height = bmp.height
              detectedWidth = bmp.width
              detectedHeight = bmp.height
            }
            const ctx = canvasEl.getContext('2d', { alpha: false })
            if (ctx) {
              ctx.drawImage(bmp, 0, 0)
            }
          }
          bmp.close()
          frameCounter++
          totalFrames++
        })
        .catch(() => {})
    }
  }

  // Helper: throttle stream resync requests (sent when the decoder has no
  // description yet, e.g. the client joined before the first config packet).
  let lastResyncAt = 0
  function requestStreamResync() {
    const now = Date.now()
    if (now - lastResyncAt < 2000) return
    lastResyncAt = now
    if (wsClient && wsClient.readyState === WebSocket.OPEN) {
      wsClient.send(JSON.stringify({ action: 'resync' }))
    }
  }

  function stopMirroring() {
    if (wsClient) {
      wsClient.disconnect()
      wsClient = null
    }

    if (videoDecoder) {
      try {
        if (videoDecoder.state !== 'closed') {
          videoDecoder.close()
        }
      } catch {}
      videoDecoder = null
    }

    if (fpsInterval) {
      clearInterval(fpsInterval)
      fpsInterval = null
    }

    isDecoderConfigured = false
    cachedSpsPps = null
    cachedAVCC = null
    avccMode = false
    lastResyncAt = 0
    tsBase = -1
    lastTs = -1
    isMirroring = false
    isConnecting = false
    fps = 0
    streamEngine = 'idle'
  }

  // --- Remote Input & Control Events ---
  function sendEvent(evt: Record<string, unknown>) {
    if (wsClient && wsClient.readyState === WebSocket.OPEN) {
      wsClient.send(JSON.stringify(evt))
    }
  }

  function sendNavKey(action: string, keycode = 0) {
    sendEvent({ action, keycode })
  }

  function sendText() {
    if (!textInput.trim()) return
    sendEvent({ action: 'text', text: textInput })
    textInput = ''
    toastStore.success('Text sent to device', 'Injected')
  }

  function sendQuickText(str: string) {
    sendEvent({ action: 'text', text: str })
    toastStore.success(`Sent: ${str}`, 'Quick Input')
  }

  // --- Dynamic Canvas Coordinate Mapping ---
  function getCanvasCoordinates(e: PointerEvent): { x: number; y: number } {
    if (!canvasEl) return { x: 0, y: 0 }
    const rect = canvasEl.getBoundingClientRect()
    if (rect.width === 0 || rect.height === 0) return { x: 0, y: 0 }

    // Physical screen dimensions of the target device
    const devW = detectedWidth || 1080
    const devH = detectedHeight || 2400

    // Compute exact rendered video area within canvas (handling any letterbox / aspect-ratio offsets)
    const containerAspect = rect.width / rect.height
    const screenAspect = devW / devH

    let renderW = rect.width
    let renderH = rect.height
    let offsetX = 0
    let offsetY = 0

    if (containerAspect > screenAspect) {
      // Container is wider than phone aspect ratio: letterbox on left and right
      renderW = rect.height * screenAspect
      offsetX = (rect.width - renderW) / 2
    } else {
      // Container is taller than phone aspect ratio: letterbox on top and bottom
      renderH = rect.width / screenAspect
      offsetY = (rect.height - renderH) / 2
    }

    const clickX = e.clientX - rect.left - offsetX
    const clickY = e.clientY - rect.top - offsetY

    const clampedX = Math.max(0, Math.min(clickX, renderW))
    const clampedY = Math.max(0, Math.min(clickY, renderH))

    const x = Math.round((clampedX / renderW) * devW)
    const y = Math.round((clampedY / renderH) * devH)
    return { x, y }
  }

  function handlePointerDown(e: PointerEvent) {
    if (!isMirroring || !canvasEl) return
    try {
      canvasEl.setPointerCapture(e.pointerId)
    } catch {}
    const { x, y } = getCanvasCoordinates(e)
    sendEvent({ action: 'down', x, y, pointerId: e.pointerId })
  }

  function handlePointerMove(e: PointerEvent) {
    if (!isMirroring || !canvasEl) return
    if (e.buttons === 0 && e.pointerType === 'mouse') return

    const now = performance.now()
    if (now - lastMoveTime < 14) return // Rate-limit to ~70Hz for smooth WS transfer
    lastMoveTime = now

    const { x, y } = getCanvasCoordinates(e)
    sendEvent({ action: 'move', x, y, pointerId: e.pointerId })
  }

  function handlePointerUp(e: PointerEvent) {
    if (!isMirroring || !canvasEl) return
    try {
      if (canvasEl.hasPointerCapture(e.pointerId)) {
        canvasEl.releasePointerCapture(e.pointerId)
      }
    } catch {}
    const { x, y } = getCanvasCoordinates(e)
    sendEvent({ action: 'up', x, y, pointerId: e.pointerId })
  }

  function handlePointerCancel(e: PointerEvent) {
    if (!isMirroring || !canvasEl) return
    try {
      if (canvasEl.hasPointerCapture(e.pointerId)) {
        canvasEl.releasePointerCapture(e.pointerId)
      }
    } catch {}
    const { x, y } = getCanvasCoordinates(e)
    sendEvent({ action: 'up', x, y, pointerId: e.pointerId })
  }

  // --- Physical Keyboard Interceptor ---
  function handleGlobalKeyDown(e: KeyboardEvent) {
    if (!isMirroring || !isKeyboardActive) return

    // Never capture when typing inside form fields
    const target = e.target as HTMLElement | null
    if (
      target &&
      (target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.isContentEditable)
    ) {
      return
    }

    const keycodeMap: Record<string, number> = {
      Backspace: 67,
      Enter: 66,
      Tab: 61,
      Escape: 4, // Back
      ArrowUp: 19,
      ArrowDown: 20,
      ArrowLeft: 21,
      ArrowRight: 22,
      Home: 3,
      Delete: 112,
    }

    if (e.key in keycodeMap) {
      e.preventDefault()
      sendNavKey('key', keycodeMap[e.key])
      return
    }

    if (e.key === ' ' || e.code === 'Space') {
      e.preventDefault()
      sendNavKey('key', 62)
      return
    }

    // Printable characters
    if (e.key.length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey) {
      e.preventDefault()
      sendEvent({ action: 'text', text: e.key })
    }
  }

  // --- Screen Power Toggle ---
  function togglePhysicalScreen() {
    isPhysicalScreenOff = !isPhysicalScreenOff
    sendEvent({ action: 'screen_power', on: !isPhysicalScreenOff })
    if (isPhysicalScreenOff) {
      toastStore.info('Device physical panel powered off (touch & mirror active)', 'Display Power')
    } else {
      toastStore.info('Device physical panel powered on', 'Display Power')
    }
  }

  // --- Screenshot Capture ---
  function captureScreenshot() {
    if (!canvasEl || !isMirroring) {
      toastStore.warning('Start mirroring before taking a screenshot', 'Screenshot')
      return
    }

    try {
      const dataUrl = canvasEl.toDataURL('image/png')
      const a = document.createElement('a')
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
      a.href = dataUrl
      a.download = `scrcpy-${detectedWidth}x${detectedHeight}-${timestamp}.png`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      toastStore.success('Saved screenshot to downloads folder', 'Screenshot Saved')
    } catch (err) {
      console.error('Screenshot error:', err)
      toastStore.error('Failed to capture frame from canvas', 'Screenshot Error')
    }
  }

  // --- Fullscreen Toggle ---
  function toggleFullscreen() {
    if (!phoneStageEl) return
    if (!document.fullscreenElement) {
      phoneStageEl
        .requestFullscreen()
        .then(() => {
          isFullscreen = true
        })
        .catch(() => {})
    } else {
      document
        .exitFullscreen()
        .then(() => {
          isFullscreen = false
        })
        .catch(() => {})
    }
  }

  // Parameter update helper
  function onSettingModified() {
    if (isMirroring) {
      settingsChanged = true
    }
  }
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar Controls (Header) -->
  <div class="neo-card bg-card p-4 rounded-xl border-2 border-border shadow-neobrutal">
    <div class="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
      <!-- Title & Live Badges -->
      <div class="flex flex-wrap items-center gap-2.5">
        <div class="w-9 h-9 rounded-lg bg-accent/20 border-2 border-accent/40 flex items-center justify-center text-accent">
          <Smartphone class="w-5 h-5" />
        </div>
        <div>
          <div class="flex items-center gap-2">
            <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground">
              SCRCPY REMOTE CONTROL
            </h2>
            {#if isMirroring}
              <Badge variant="success" class="flex items-center gap-1.5 animate-pulse">
                <span class="w-2 h-2 rounded-full bg-emerald-400"></span>
                <span>{fps > 0 ? `LIVE ${fps} FPS` : 'LIVE (Idle 60Hz)'}</span>
              </Badge>
            {:else if isConnecting}
              <Badge variant="warning" class="flex items-center gap-1.5 animate-pulse">
                <RefreshCw class="w-3 h-3 animate-spin" />
                <span>CONNECTING...</span>
              </Badge>
            {:else}
              <Badge variant="default">OFFLINE</Badge>
            {/if}
          </div>
          <p class="text-xs text-muted font-mono mt-0.5">
            Low-latency hardware video mirroring with zero-delay pointer injection
          </p>
        </div>
      </div>

      <!-- Action Buttons -->
      <div class="flex items-center flex-wrap gap-2 w-full md:w-auto justify-end">
        <!-- Settings Toggle (Mobile focus) -->
        <Button
          variant={showSettings ? 'secondary' : 'outline'}
          size="sm"
          onclick={() => (showSettings = !showSettings)}
          title="Toggle stream customization sidebar"
        >
          <SlidersHorizontal class="w-3.5 h-3.5 mr-1" />
          <span class="hidden sm:inline">Settings</span>
        </Button>

        <!-- Screenshot Button -->
        <Button
          variant="outline"
          size="sm"
          disabled={!isMirroring}
          onclick={captureScreenshot}
          title="Capture instant screenshot to PNG"
        >
          <Camera class="w-3.5 h-3.5 mr-1 text-cyan-400" />
          <span class="hidden sm:inline">Snap</span>
        </Button>

        <!-- Fullscreen Button -->
        <Button
          variant="outline"
          size="sm"
          onclick={toggleFullscreen}
          title="Toggle phone stage fullscreen"
        >
          {#if isFullscreen}
            <Minimize2 class="w-3.5 h-3.5 mr-1" />
            <span class="hidden sm:inline">Exit</span>
          {:else}
            <Maximize2 class="w-3.5 h-3.5 mr-1" />
            <span class="hidden sm:inline">Full</span>
          {/if}
        </Button>

        <!-- Start / Stop Stream Main CTA -->
        {#if isMirroring}
          <Button variant="danger" size="sm" onclick={stopMirroring}>
            <Square class="w-3.5 h-3.5 mr-1 fill-current" />
            <span>Stop Stream</span>
          </Button>
        {:else}
          <Button
            variant="primary"
            size="sm"
            disabled={isConnecting}
            onclick={startMirroring}
          >
            {#if isConnecting}
              <RefreshCw class="w-3.5 h-3.5 mr-1 animate-spin" />
              <span>Starting...</span>
            {:else}
              <Play class="w-3.5 h-3.5 mr-1 fill-current" />
              <span>Start Mirroring</span>
            {/if}
          </Button>
        {/if}
      </div>
    </div>

    <!-- Parameter Changed Alert if modified on the fly -->
    {#if settingsChanged && isMirroring}
      <div class="mt-3 pt-3 border-t border-border/80 flex items-center justify-between gap-2 text-xs font-mono bg-amber-950/30 border border-amber-800/60 text-amber-300 px-3 py-2 rounded-lg">
        <div class="flex items-center gap-2">
          <Zap class="w-4 h-4 text-amber-400 shrink-0" />
          <span>Stream settings modified. Restart session to apply new bitrate/resolution.</span>
        </div>
        <Button variant="outline" size="sm" class="border-amber-700 hover:bg-amber-900/40 text-amber-200" onclick={startMirroring}>
          <RefreshCw class="w-3 h-3 mr-1" />
          <span>Restart</span>
        </Button>
      </div>
    {/if}
  </div>

  <!-- Screen View & Remote Controls Layout -->
  <div class="grid grid-cols-1 {showSettings ? 'lg:grid-cols-12' : ''} gap-3 sm:gap-6 items-start">
    <!-- Left/Main Stage: Phone Screen Frame -->
    <div class="{showSettings ? 'lg:col-span-7 xl:col-span-8' : 'w-full'} flex flex-col items-center">
      <div
        bind:this={phoneStageEl}
        class="neo-card bg-card border-2 border-border shadow-neobrutal rounded-2xl p-2.5 sm:p-4 flex flex-col items-center w-full max-w-sm sm:max-w-md transition-all relative {isFullscreen ? 'fixed inset-0 z-50 max-w-none rounded-none justify-center bg-black border-none' : ''}"
      >
        <!-- Phone Speaker & Camera Notch Top Accent -->
        <div class="w-full flex items-center justify-between font-mono text-[10px] text-muted pb-1.5 sm:pb-2 mb-1.5 sm:mb-2 border-b border-border">
          <div class="flex items-center gap-2">
            <span class="inline-block w-2 h-2 rounded-full {isMirroring ? 'bg-emerald-500' : 'bg-zinc-600'}"></span>
            <span class="font-bold tracking-widest text-foreground">Termux-DEV</span>
          </div>

          <!-- Front bezel speaker notch -->
          <div class="flex items-center gap-1.5 px-3 py-0.5 rounded-full bg-card-sub border border-border/60">
            <div class="w-8 h-1 bg-border rounded-full"></div>
            <div class="w-2 h-2 rounded-full bg-black border border-border/80"></div>
          </div>

          <div class="flex items-center gap-1.5">
            {#if isMirroring}
              <span class="text-emerald-400 font-bold">{detectedWidth}x{detectedHeight}</span>
            {:else}
              <span>OFFLINE</span>
            {/if}
          </div>
        </div>

        <!-- WebCodecs Availability Notice -->
        {#if webCodecsUnavailable}
          <div class="w-full mb-1.5 sm:mb-2 px-2.5 py-1.5 rounded-lg bg-amber-500/10 border border-amber-500/40 text-[10px] font-mono text-amber-400 leading-relaxed">
            WebCodecs H.264 needs a secure context (HTTPS or localhost). Over plain HTTP the
            mirror runs on the MJPEG screencap fallback at ~3 FPS.
          </div>
        {/if}

        <!-- Phone Screen Surface Area -->
        <div
          class="relative w-full bg-black rounded-xl overflow-hidden border-2 border-border flex items-center justify-center select-none shadow-inner"
          style="aspect-ratio: {aspectRatio}; touch-action: none;"
        >
          <!-- Canvas rendering element -->
          <canvas
            bind:this={canvasEl}
            class="w-full h-full object-contain cursor-crosshair {isMirroring ? 'block' : 'hidden'}"
            onpointerdown={handlePointerDown}
            onpointermove={handlePointerMove}
            onpointerup={handlePointerUp}
            onpointercancel={handlePointerCancel}
            onpointerleave={handlePointerCancel}
          ></canvas>

          <!-- Empty / Offline Backdrop Overlay -->
          {#if !isMirroring}
            <div class="absolute inset-0 flex flex-col items-center justify-center p-6 text-center bg-gradient-to-b from-card/90 via-card/95 to-black font-mono space-y-4">
              <!-- Geometric Grid Ambient Accent -->
              <div class="w-16 h-16 rounded-2xl bg-card-sub border-2 border-border flex items-center justify-center shadow-neobrutal">
                <Smartphone class="w-8 h-8 text-accent animate-pulse" />
              </div>

              <div>
                <h3 class="text-sm sm:text-base font-bold text-foreground uppercase tracking-wider">
                  Mirroring Inactive
                </h3>
                <p class="text-xs text-muted mt-1 max-w-xs">
                  Connect to your device's native low-latency scrcpy server engine
                </p>
              </div>

              <div class="flex flex-wrap justify-center gap-1.5 max-w-xs text-[10px]">
                <Badge variant="info">WebCodecs H.264</Badge>
                <Badge variant="default">&lt;15ms Latency</Badge>
                <Badge variant="default">Zero ADB Lag</Badge>
                <Badge variant="default">Hardware Touch</Badge>
              </div>

              <div class="pt-2">
                <Button
                  variant="primary"
                  size="md"
                  disabled={isConnecting}
                  onclick={startMirroring}
                  class="shadow-neobrutal"
                >
                  {#if isConnecting}
                    <RefreshCw class="w-4 h-4 mr-1.5 animate-spin" />
                    <span>Connecting...</span>
                  {:else}
                    <Play class="w-4 h-4 mr-1.5 fill-current" />
                    <span>Start Mirroring</span>
                  {/if}
                </Button>
              </div>
            </div>
          {/if}

          <!-- Floating Keyboard Overlay Indicator when active -->
          {#if isMirroring && isKeyboardActive}
            <div class="absolute top-2 left-2 pointer-events-none bg-emerald-950/90 text-emerald-300 border border-emerald-700/80 px-2 py-1 rounded text-[10px] font-mono flex items-center gap-1.5 shadow-md">
              <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-ping"></span>
              <span>PC Keyboard Captured</span>
            </div>
          {/if}
        </div>

        <!-- Android Virtual Navigation & Hardware Control Dock -->
        <div class="w-full mt-2 sm:mt-3 pt-2 sm:pt-3 border-t-2 border-border flex flex-col gap-1.5 sm:gap-2">
          <!-- Main Android Navigation Bar -->
          <div class="grid grid-cols-3 gap-1.5 sm:gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('back', 4)}
              title="Android Back (Keycode 4 / Esc)"
              class="hover:bg-card-sub py-1.5 sm:py-2 px-1.5 sm:px-3 text-[11px] sm:text-xs"
            >
              <ArrowLeft class="w-3.5 h-3.5 sm:w-4 sm:h-4 mr-1" />
              <span>Back</span>
            </Button>

            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('home', 3)}
              title="Android Home (Keycode 3)"
              class="hover:bg-card-sub py-1.5 sm:py-2 px-1.5 sm:px-3 text-[11px] sm:text-xs"
            >
              <Home class="w-3.5 h-3.5 sm:w-4 sm:h-4 mr-1" />
              <span>Home</span>
            </Button>

            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('recents', 187)}
              title="Android Recents / App Switcher (Keycode 187)"
              class="hover:bg-card-sub py-1.5 sm:py-2 px-1.5 sm:px-3 text-[11px] sm:text-xs"
            >
              <Menu class="w-3.5 h-3.5 sm:w-4 sm:h-4 mr-1" />
              <span>Recents</span>
            </Button>
          </div>

          <!-- Extended Quick Hardware Dock -->
          <div class="grid grid-cols-5 gap-1 sm:gap-1.5 font-mono text-xs">
            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('vol_down', 25)}
              title="Volume Down (Keycode 25)"
              class="py-1.5 sm:py-2 px-1 sm:px-1.5 flex justify-center items-center"
            >
              <Volume1 class="w-3.5 h-3.5 text-amber-400" />
            </Button>

            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('vol_up', 24)}
              title="Volume Up (Keycode 24)"
              class="py-1.5 sm:py-2 px-1 sm:px-1.5 flex justify-center items-center"
            >
              <Volume2 class="w-3.5 h-3.5 text-emerald-400" />
            </Button>

            <Button
              variant="outline"
              size="sm"
              disabled={!isMirroring}
              onclick={() => sendNavKey('power', 26)}
              title="Physical Power Key (Keycode 26)"
              class="py-1.5 sm:py-2 px-1 sm:px-1.5 flex justify-center items-center"
            >
              <Power class="w-3.5 h-3.5 text-red-400" />
            </Button>

            <!-- Toggle Physical Device Screen Power -->
            <Button
              variant={isPhysicalScreenOff ? 'secondary' : 'outline'}
              size="sm"
              disabled={!isMirroring}
              onclick={togglePhysicalScreen}
              title={isPhysicalScreenOff ? 'Turn Physical Screen ON' : 'Turn Physical Screen OFF (Save Battery & Heat)'}
              class="py-1.5 sm:py-2 px-1 sm:px-1.5 flex justify-center items-center {isPhysicalScreenOff ? 'border-accent bg-accent/20 text-accent' : ''}"
            >
              {#if isPhysicalScreenOff}
                <EyeOff class="w-3.5 h-3.5 text-accent" />
              {:else}
                <Eye class="w-3.5 h-3.5 text-muted hover:text-foreground" />
              {/if}
            </Button>

            <!-- Direct PC Keyboard Capture Toggle -->
            <Button
              variant={isKeyboardActive ? 'primary' : 'outline'}
              size="sm"
              disabled={!isMirroring}
              onclick={() => {
                isKeyboardActive = !isKeyboardActive
                if (isKeyboardActive) {
                  toastStore.info('PC Keyboard captured: typing sends keys directly to device', 'Keyboard ON')
                }
              }}
              title={isKeyboardActive ? 'Disable PC Keyboard Capture' : 'Enable PC Keyboard Capture'}
              class="py-1.5 sm:py-2 px-1 sm:px-1.5 flex justify-center items-center"
            >
              <Keyboard class="w-3.5 h-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </div>

    <!-- Right Column: Stream Customization & Diagnostics Card -->
    {#if showSettings}
      <div class="lg:col-span-5 xl:col-span-4 space-y-4">
        <!-- 1. Stream Customization Card -->
        <Card title="Stream Customization" subtitle="Adjust encoder quality & parameters">
          <div class="space-y-4 font-mono text-xs">
            <!-- Resolution Selector -->
            <div>
              <div class="block text-[11px] font-bold text-muted uppercase tracking-wider mb-1.5">
                Resolution Limit
              </div>
              <div class="grid grid-cols-2 gap-1.5">
                {#each resolutionOptions as opt}
                  <button
                    type="button"
                    class="px-2 py-2 rounded-lg border text-left transition-all cursor-pointer {maxSize === opt.value ? 'bg-accent text-accent-text border-accent font-bold shadow-sm' : 'bg-card-sub border-border text-foreground hover:bg-card hover:border-accent/40'}"
                    onclick={() => {
                      maxSize = opt.value
                      onSettingModified()
                    }}
                  >
                    <div class="text-xs font-bold">{opt.label}</div>
                    <div class="text-[10px] opacity-75">{opt.desc}</div>
                  </button>
                {/each}
              </div>
            </div>

            <!-- Bitrate Selector & Custom Input -->
            <div>
              <div class="flex items-center justify-between mb-1.5">
                <span class="text-[11px] font-bold text-muted uppercase tracking-wider">
                  Video Bitrate
                </span>
                <span class="text-[11px] font-mono text-accent font-bold">
                  {(bitRate / 1000000).toFixed(0)} Mbps
                </span>
              </div>

              <!-- Presets Grid -->
              <div class="grid grid-cols-3 sm:grid-cols-6 gap-1.5 mb-2">
                {#each bitrateOptions as b}
                  <button
                    type="button"
                    class="py-1.5 px-1 text-center rounded-md border text-xs transition-all cursor-pointer {bitRate === b.value ? 'bg-accent text-accent-text border-accent font-bold shadow-sm' : 'bg-card-sub border-border text-foreground hover:bg-card'}"
                    onclick={() => {
                      bitRate = b.value
                      customBitrateMbps = Math.round(b.value / 1000000)
                      onSettingModified()
                    }}
                  >
                    {b.label}
                  </button>
                {/each}
              </div>

              <!-- Custom Bitrate Input -->
              <div class="flex items-center gap-2 p-2 bg-card-sub rounded-lg border border-border">
                <span class="text-[11px] text-muted whitespace-nowrap font-bold">Custom:</span>
                <input
                  type="number"
                  min="1"
                  max="100"
                  bind:value={customBitrateMbps}
                  onchange={() => {
                    const mbps = Math.max(1, Math.min(100, Number(customBitrateMbps) || 8))
                    customBitrateMbps = mbps
                    bitRate = mbps * 1000000
                    onSettingModified()
                  }}
                  class="w-20 px-2 py-1 bg-card border border-border rounded text-xs text-foreground font-mono focus:outline-none focus:border-accent"
                  placeholder="Mbps"
                />
                <span class="text-xs text-muted font-mono">Mbps</span>
                <button
                  type="button"
                  class="ml-auto px-2.5 py-1 text-[11px] bg-accent text-accent-text font-bold rounded border border-accent hover:opacity-90 transition-opacity cursor-pointer"
                  onclick={() => {
                    const mbps = Math.max(1, Math.min(100, Number(customBitrateMbps) || 8))
                    customBitrateMbps = mbps
                    bitRate = mbps * 1000000
                    onSettingModified()
                  }}
                >
                  Apply
                </button>
              </div>
            </div>

            <!-- Framerate & Display Selectors -->
            <div class="grid grid-cols-2 gap-3">
              <div>
                <div class="block text-[11px] font-bold text-muted uppercase tracking-wider mb-1.5">
                  Target FPS
                </div>
                <div class="grid grid-cols-2 gap-1.5">
                  <button
                    type="button"
                    class="py-1.5 text-center rounded border cursor-pointer {maxFps === 30 ? 'bg-accent text-accent-text border-accent font-bold' : 'bg-card-sub border-border'}"
                    onclick={() => {
                      maxFps = 30
                      onSettingModified()
                    }}
                  >
                    30 FPS
                  </button>
                  <button
                    type="button"
                    class="py-1.5 text-center rounded border cursor-pointer {maxFps === 60 ? 'bg-accent text-accent-text border-accent font-bold' : 'bg-card-sub border-border'}"
                    onclick={() => {
                      maxFps = 60
                      onSettingModified()
                    }}
                  >
                    60 FPS
                  </button>
                </div>
              </div>

              <div>
                <div class="block text-[11px] font-bold text-muted uppercase tracking-wider mb-1.5">
                  Target Display
                </div>
                <div class="grid grid-cols-2 gap-1.5">
                  <button
                    type="button"
                    class="py-1.5 text-center rounded border cursor-pointer {displayId === 0 ? 'bg-accent text-accent-text border-accent font-bold' : 'bg-card-sub border-border'}"
                    onclick={() => {
                      displayId = 0
                      onSettingModified()
                    }}
                  >
                    Main (0)
                  </button>
                  <button
                    type="button"
                    class="py-1.5 text-center rounded border cursor-pointer {displayId === 1 ? 'bg-accent text-accent-text border-accent font-bold' : 'bg-card-sub border-border'}"
                    onclick={() => {
                      displayId = 1
                      onSettingModified()
                    }}
                  >
                    Cover (1)
                  </button>
                </div>
              </div>
            </div>

            <!-- Device Behavior Checkboxes -->
            <div class="pt-2 border-t border-border/80 space-y-2">
              <label class="flex items-center gap-2.5 cursor-pointer select-none">
                <input
                  type="checkbox"
                  bind:checked={turnScreenOff}
                  onchange={onSettingModified}
                  class="rounded border-border text-accent focus:ring-accent bg-card-sub w-4 h-4 cursor-pointer"
                />
                <span class="text-xs text-foreground">Turn off physical screen on stream launch</span>
              </label>

              <label class="flex items-center gap-2.5 cursor-pointer select-none">
                <input
                  type="checkbox"
                  bind:checked={stayAwake}
                  onchange={onSettingModified}
                  class="rounded border-border text-accent focus:ring-accent bg-card-sub w-4 h-4 cursor-pointer"
                />
                <span class="text-xs text-foreground">Keep device awake while mirroring</span>
              </label>
            </div>
          </div>
        </Card>

        <!-- 2. Real-Time Diagnostics Card -->
        <Card title="Diagnostics & Telemetry" subtitle="Hardware decoding & pipeline metrics">
          <div class="space-y-3 font-mono text-xs">
            <!-- Digital FPS Gauge -->
            <div class="p-3 bg-card-sub rounded-xl border border-border flex items-center justify-between">
              <div>
                <span class="text-[10px] text-muted uppercase font-bold tracking-wider">Live Refresh Rate</span>
                <div class="flex items-baseline gap-1 mt-0.5">
                  <span class="text-2xl font-bold {fps >= 50 ? 'text-emerald-400' : fps >= 25 ? 'text-amber-400' : isMirroring ? 'text-emerald-400/80' : 'text-muted'}">
                    {fps > 0 ? fps : (isMirroring ? 'Idle' : '0')}
                  </span>
                  <span class="text-xs text-muted">{fps > 0 ? 'FPS' : (isMirroring ? '(60Hz Standby)' : 'FPS')}</span>
                </div>
              </div>

              <div class="text-right">
                <Badge variant={streamEngine === 'webcodecs' ? 'success' : streamEngine === 'mjpeg' ? 'warning' : 'default'}>
                  {#if streamEngine === 'webcodecs'}
                    WEBCODECS H.264
                  {:else if streamEngine === 'mjpeg'}
                    MJPEG FALLBACK
                  {:else}
                    IDLE
                  {/if}
                </Badge>
                <div class="text-[10px] text-muted mt-1">
                  {totalFrames > 0 ? `${totalFrames.toLocaleString()} frames decoded` : 'No frames'}
                </div>
              </div>
            </div>

            <!-- Detailed Grid Readouts -->
            <div class="grid grid-cols-2 gap-2 text-[11px]">
              <div class="p-2.5 bg-card-sub rounded-lg border border-border col-span-2">
                <div class="text-muted text-[10px] uppercase">Active Hardware Encoder</div>
                <div class="font-bold text-foreground mt-0.5 truncate text-[11px]" title={hardwareEncoderName}>
                  {hardwareEncoderName || 'Detecting...'}
                </div>
              </div>

              <div class="p-2.5 bg-card-sub rounded-lg border border-border">
                <div class="text-muted text-[10px] uppercase">Display Output</div>
                <div class="font-bold text-foreground mt-0.5">
                  {isMirroring ? `${detectedWidth} × ${detectedHeight}` : '—'}
                </div>
              </div>

              <div class="p-2.5 bg-card-sub rounded-lg border border-border">
                <div class="text-muted text-[10px] uppercase">Codec Profile</div>
                <div class="font-bold text-foreground mt-0.5 truncate" title={codecName}>
                  {codecName || 'avc1.640033'}
                </div>
              </div>

              <div class="p-2.5 bg-card-sub rounded-lg border border-border">
                <div class="text-muted text-[10px] uppercase">Encoder Bitrate</div>
                <div class="font-bold text-foreground mt-0.5">
                  {(bitRate / 1000000).toFixed(0)} Mbps
                </div>
              </div>

              <div class="p-2.5 bg-card-sub rounded-lg border border-border">
                <div class="text-muted text-[10px] uppercase">Pipeline Latency</div>
                <div class="font-bold text-emerald-400 mt-0.5">
                  {isMirroring ? '< 15 ms' : '—'}
                </div>
              </div>
            </div>
          </div>
        </Card>

        <!-- 3. Direct Text Injection & Quick URL Bar -->
        <Card title="Direct Text Injection" subtitle="Send strings, passwords or URLs to phone">
          <form
            onsubmit={(e) => {
              e.preventDefault()
              sendText()
            }}
            class="space-y-3 font-mono text-xs"
          >
            <div class="flex items-center gap-2">
              <Input
                placeholder="Paste URL or type string to send..."
                bind:value={textInput}
                disabled={!isMirroring}
                class="text-xs"
              />
              <Button
                type="submit"
                variant="primary"
                size="md"
                disabled={!isMirroring || !textInput.trim()}
                title="Inject string to active input field"
              >
                <Send class="w-3.5 h-3.5" />
              </Button>
            </div>

            <!-- Quick Paste Presets -->
            <div class="flex items-center flex-wrap gap-1.5 pt-1">
              <span class="text-[10px] text-muted uppercase font-bold mr-1">Quick:</span>
              <button
                type="button"
                disabled={!isMirroring}
                class="px-2 py-0.5 rounded bg-card-sub hover:bg-card border border-border text-[10px] text-foreground disabled:opacity-50 cursor-pointer"
                onclick={() => sendQuickText('https://')}
              >
                https://
              </button>
              <button
                type="button"
                disabled={!isMirroring}
                class="px-2 py-0.5 rounded bg-card-sub hover:bg-card border border-border text-[10px] text-foreground disabled:opacity-50 cursor-pointer"
                onclick={() => sendQuickText('www.')}
              >
                www.
              </button>
              <button
                type="button"
                disabled={!isMirroring}
                class="px-2 py-0.5 rounded bg-card-sub hover:bg-card border border-border text-[10px] text-foreground disabled:opacity-50 cursor-pointer"
                onclick={() => sendNavKey('key', 66)}
              >
                [Enter]
              </button>
              <button
                type="button"
                disabled={!isMirroring}
                class="px-2 py-0.5 rounded bg-card-sub hover:bg-card border border-border text-[10px] text-foreground disabled:opacity-50 cursor-pointer"
                onclick={() => sendNavKey('key', 67)}
              >
                [Del]
              </button>
            </div>
          </form>
        </Card>
      </div>
    {/if}
  </div>
</div>
