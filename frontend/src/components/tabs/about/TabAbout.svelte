<script lang="ts">
  import {
    Info,
    Zap,
    ExternalLink,
    Code2,
    Sliders,
    BatteryCharging,
    MessageSquare,
    Tv,
    Activity,
    Network,
    Terminal,
  } from '@lucide/svelte'
  import Card from '../../ui/Card.svelte'
  import Badge from '../../ui/Badge.svelte'
  import { APP_BUILD, APP_VERSION } from '../../../config/version'

  const REPO_URL = 'https://github.com/sainz1407/termux-web'
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- 1. Header Hero -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-2 border-b border-border font-mono">
    <div class="flex items-center gap-2.5 flex-wrap">
      <div class="p-2 rounded bg-card-sub border border-border text-accent">
        <Info class="w-5 h-5" />
      </div>
      <div class="flex items-center gap-2 flex-wrap">
        <h2 class="text-base sm:text-lg font-bold uppercase tracking-wider text-foreground">
          About Termux Monitor
        </h2>
        <span class="px-2 py-0.5 rounded text-[10px] font-black uppercase tracking-wider bg-amber-400 text-black shadow-neobrutal-sm">
          PRO
        </span>
        <Badge variant="default" class="text-[10px]">
          {APP_VERSION} (Build {APP_BUILD})
        </Badge>
      </div>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <a
        href={REPO_URL}
        target="_blank"
        rel="noreferrer"
        class="neo-button inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono font-bold rounded bg-card-sub border border-border text-foreground hover:border-accent hover:text-accent cursor-pointer transition-colors"
      >
        <Code2 class="w-4 h-4 text-accent" />
        <span>GitHub Repository</span>
        <ExternalLink class="w-3 h-3 text-muted" />
      </a>
      <a
        href={`${REPO_URL}/issues`}
        target="_blank"
        rel="noreferrer"
        class="neo-button inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono font-bold rounded bg-card-sub border border-border text-foreground hover:border-sky-400 hover:text-sky-400 cursor-pointer transition-colors"
      >
        <MessageSquare class="w-4 h-4 text-sky-400" />
        <span>Report Issue</span>
        <ExternalLink class="w-3 h-3 text-muted" />
      </a>
    </div>
  </div>

  <!-- Main Content Grid -->
  <div class="space-y-3 sm:space-y-6">
    <!-- Top Row: Card 1 & Card 2 in 2-Column Desktop Grid -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3 sm:gap-6 items-start">
      <!-- 2. Card 1: Project Overview & Specs (tone="ice") -->
      <Card
        title="Project Overview & Specs"
        subtitle="Android System Control Panel & WebUI"
        tone="ice"
      >
        <div class="space-y-4 font-mono text-xs">
          <!-- Logo Box & Tagline -->
          <div class="flex items-center gap-3.5 p-3.5 bg-card-sub border border-border rounded-lg">
            <div class="w-12 h-12 rounded-lg bg-accent text-accent-text border-2 border-border shadow-neobrutal-sm flex items-center justify-center font-black text-2xl font-mono shrink-0 select-none">
              T
            </div>
            <div>
              <h3 class="text-sm sm:text-base font-black uppercase tracking-wider text-foreground">
                Termux Monitor
              </h3>
              <p class="text-[11px] text-muted mt-0.5">
                High-Performance Android System WebUI & Control Panel
              </p>
            </div>
          </div>

          <!-- 4 Specifications Grid -->
          <div class="grid grid-cols-2 sm:grid-cols-2 gap-2 sm:gap-2.5">
            <div class="p-2.5 sm:p-3 bg-card-sub border border-border rounded space-y-0.5 sm:space-y-1">
              <span class="text-[10px] uppercase font-bold text-muted">App Version</span>
              <p class="font-bold text-accent">Go {APP_VERSION} ({APP_BUILD})</p>
            </div>
            <div class="p-2.5 sm:p-3 bg-card-sub border border-border rounded space-y-0.5 sm:space-y-1">
              <span class="text-[10px] uppercase font-bold text-muted">Runtime</span>
              <p class="font-bold text-foreground truncate">Magisk / KernelSU / APatch</p>
            </div>

            <div class="p-2.5 sm:p-3 bg-card-sub border border-border rounded space-y-0.5 sm:space-y-1">
              <span class="text-[10px] uppercase font-bold text-muted">Maintainer</span>
              <p class="font-bold text-foreground">sainz1407</p>
            </div>

            <div class="p-2.5 sm:p-3 bg-card-sub border border-border rounded space-y-0.5 sm:space-y-1">
              <span class="text-[10px] uppercase font-bold text-muted">License</span>
              <p class="font-bold text-emerald-400">GNU GPL v3.0</p>
            </div>
          </div>

          <!-- Architecture Paragraph -->
          <p class="leading-relaxed text-muted p-3 bg-card-sub border border-border rounded text-[11px]">
            Modular Go backend with a <strong class="text-foreground">Svelte 5</strong> + <strong class="text-foreground">Tailwind CSS</strong> frontend embedded directly into a single self-contained binary, delivering near-zero idle CPU usage and a tiny memory footprint. Fully offline-capable with no external CDN or cloud dependency.
          </p>
        </div>
      </Card>

      <!-- 3. Card 2: Key Features (tone="mint") -->
      <Card
        title="Key Features"
        subtitle="Kernel-level tuning & automated background engine"
        tone="mint"
      >
        <div class="space-y-2.5 font-mono text-xs">
          <!-- Feature 1: Network Tuning -->
          <div class="p-3 bg-card-sub border border-border rounded space-y-1 hover:border-accent/40 transition-colors">
            <div class="flex items-center gap-2 text-foreground font-bold">
              <Zap class="w-4 h-4 text-amber-400 shrink-0" />
              <span>TCP Congestion & Network Tuning</span>
            </div>
            <p class="text-[11px] text-muted leading-relaxed pl-6">
              Configurable congestion control algorithm selection paired with persistent sysctl profiles tuned for low latency and high bandwidth wireless networks.
            </p>
          </div>

          <!-- Feature 2: Optimizer Tweaks -->
          <div class="p-3 bg-card-sub border border-border rounded space-y-1 hover:border-accent/40 transition-colors">
            <div class="flex items-center gap-2 text-foreground font-bold">
              <Sliders class="w-4 h-4 text-blue-400 shrink-0" />
              <span>System Optimizer Tweaks</span>
            </div>
            <p class="text-[11px] text-muted leading-relaxed pl-6">
              Kernel sysctl variable configuration, TCP FastOpen, queue limit allocation, DNS options and dynamic SDK-aware TTL handling — all driven by a single tunables config.
            </p>
          </div>

          <!-- Feature 3: Charge Limiter -->
          <div class="p-3 bg-card-sub border border-border rounded space-y-1 hover:border-accent/40 transition-colors">
            <div class="flex items-center gap-2 text-foreground font-bold">
              <BatteryCharging class="w-4 h-4 text-emerald-400 shrink-0" />
              <span>Dynamic Charge Limiter</span>
            </div>
            <p class="text-[11px] text-muted leading-relaxed pl-6">
              Automatic detection of battery charge-limit nodes via sysfs across vendor implementations, preventing battery degradation on long-term powered devices.
            </p>
          </div>

          <!-- Feature 4: Hotspot Control -->
          <div class="p-3 bg-card-sub border border-border rounded space-y-1 hover:border-accent/40 transition-colors">
            <div class="flex items-center gap-2 text-foreground font-bold">
              <Network class="w-4 h-4 text-cyan-400 shrink-0" />
              <span>Hotspot Control</span>
            </div>
            <p class="text-[11px] text-muted leading-relaxed pl-6">
              SoftAP control with connected client discovery and MAC filtering.
            </p>
          </div>

          <!-- Feature 5: Terminal & Screen Mirroring -->
          <div class="p-3 bg-card-sub border border-border rounded space-y-1 hover:border-accent/40 transition-colors">
            <div class="flex items-center gap-2 text-foreground font-bold">
              <Tv class="w-4 h-4 text-purple-400 shrink-0" />
              <span>WebSocket Terminal & Screen Mirroring</span>
            </div>
            <p class="text-[11px] text-muted leading-relaxed pl-6">
              Interactive low-latency shell sessions with canvas-based screen rendering and touch input/swipe injection support.
            </p>
          </div>
        </div>
      </Card>
    </div>

    <!-- 4. Card 3: Module Coverage -->
    <Card
      title="Module Coverage"
      subtitle="Built-in panels shipped with the module"
      tone="peach"
    >
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 font-mono text-xs">
        <div class="p-3 bg-card-sub border border-border rounded space-y-1">
          <div class="flex items-center gap-2 text-foreground font-bold">
            <Activity class="w-4 h-4 text-red-400 shrink-0" />
            <span>System & Diagnostics</span>
          </div>
          <p class="text-[11px] text-muted leading-relaxed">
            Live CPU, memory, storage and thermal telemetry, structured log viewer and module state inspection.
          </p>
        </div>
        <div class="p-3 bg-card-sub border border-border rounded space-y-1">
          <div class="flex items-center gap-2 text-foreground font-bold">
            <MessageSquare class="w-4 h-4 text-amber-400 shrink-0" />
            <span>Telephony & SMS</span>
          </div>
          <p class="text-[11px] text-muted leading-relaxed">
            Cellular signal and SIM status telemetry alongside modem SMS retrieval and inbox browser.
          </p>
        </div>
        <div class="p-3 bg-card-sub border border-border rounded space-y-1">
          <div class="flex items-center gap-2 text-foreground font-bold">
            <Terminal class="w-4 h-4 text-emerald-400 shrink-0" />
            <span>Utilities</span>
          </div>
          <p class="text-[11px] text-muted leading-relaxed">
            File manager with archive handling, Dropbear SSH client manager, power actions and remote scrcpy.
          </p>
        </div>
      </div>

      <!-- Repository Links Row -->
      <div class="mt-4 pt-4 border-t border-border">
        <div class="flex items-center justify-between gap-3 flex-wrap">
          <div class="text-[11px] font-mono text-muted">
            <span class="font-bold text-foreground">Project Source:</span> Issues, feature requests and releases are handled on GitHub.
          </div>
          <div class="flex items-center gap-2 flex-wrap">
            <a
              href={REPO_URL}
              target="_blank"
              rel="noreferrer"
              class="neo-button inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono font-bold rounded bg-card border border-border text-foreground hover:border-accent hover:text-accent cursor-pointer transition-colors"
            >
              <Code2 class="w-3.5 h-3.5 text-accent" />
              <span>GitHub Repo</span>
              <ExternalLink class="w-3 h-3 text-muted" />
            </a>
            <a
              href={`${REPO_URL}/releases`}
              target="_blank"
              rel="noreferrer"
              class="neo-button inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono font-bold rounded bg-card border border-border text-foreground hover:border-sky-400 hover:text-sky-400 cursor-pointer transition-colors"
            >
              <Zap class="w-3.5 h-3.5 text-sky-400" />
              <span>Releases</span>
              <ExternalLink class="w-3 h-3 text-muted" />
            </a>
          </div>
        </div>
      </div>
    </Card>
  </div>
</div>
