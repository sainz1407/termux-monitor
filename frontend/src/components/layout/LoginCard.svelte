<script lang="ts">
  import {
    Lock,
    Eye,
    EyeOff,
    Palette,
    ArrowRight,
    ShieldCheck,
    Check,
  } from '@lucide/svelte'
  import { authStore } from '../../stores/auth.svelte'
  import { themeStore } from '../../stores/theme.svelte'
  import { sysinfoStore } from '../../stores/sysinfo.svelte'
  import AppearanceModal from './AppearanceModal.svelte'

  let password = $state('')
  let showPassword = $state(false)
  let showAppearance = $state(false)

  async function handleSubmit(e: Event) {
    e.preventDefault()
    if (!password.trim() || authStore.isLoading) return
    await authStore.login({ password: password.trim() })
  }

  function autofillDefault() {
    password = 'tmon'
  }
</script>

<div class="min-h-screen flex flex-col justify-between bg-background text-foreground relative font-sans select-none overflow-x-hidden">
  <!-- Static Independent Login Wallpaper Layer -->
  {#if themeStore.loginWallpaper && themeStore.loginWallpaper !== 'preset:none'}
    <div
      class="login-wallpaper-bg fixed inset-0 pointer-events-none z-0 bg-cover bg-center bg-no-repeat transition-all duration-300"
      style="
        background-image: {themeStore.getLoginWallpaperBackground()};
        filter: brightness({1 - themeStore.loginWallpaperDim}) blur({themeStore.loginWallpaperBlur}px);
        transform: scale(1.04);
      "
    ></div>
    <div class="fixed inset-0 pointer-events-none z-0 bg-black/25"></div>
  {/if}

  <!-- Top Minimal Header -->
  <header class="relative z-10 w-full max-w-7xl mx-auto px-4 py-4 flex items-center justify-between">
    <!-- Brand Info -->
    <div class="flex items-center gap-2.5">
      <div class="w-8 h-8 rounded-lg bg-accent text-accent-text border-2 border-border flex items-center justify-center font-black text-sm shadow-[2px_2px_0px_0px_var(--neo-shadow)] shrink-0">
        T
      </div>
      <div class="flex items-center gap-2">
        <span class="font-mono font-black text-sm tracking-wider uppercase text-foreground">
          Termux Monitor
        </span>
        <span class="px-1.5 py-0.5 rounded text-[10px] font-mono font-black uppercase bg-accent/20 text-accent border border-accent/40">
          PRO
        </span>
      </div>
    </div>
    <!-- Appearance Customization Button (Pre-auth customization) -->
    <button
      type="button"
      class="flex items-center gap-1.5 px-3 py-1.5 font-mono text-xs font-bold uppercase transition-all cursor-pointer {themeStore.currentStyle === 'neobrutal' ? 'bg-card border-2 border-border shadow-neobrutal-sm text-foreground hover:bg-card-sub rounded active:translate-x-0.5 active:translate-y-0.5' : 'bg-card/90 backdrop-blur-md border border-border text-foreground hover:bg-card-sub rounded-xl shadow-sm hover:shadow active:scale-95'}"
      onclick={() => (showAppearance = true)}
      title="Appearance & Theme Customization"
    >
      <Palette class="w-3.5 h-3.5 text-accent" />
      <span class="hidden sm:inline">Appearance</span>
      <span class="sm:hidden">Theme</span>
    </button>
  </header>

  <!-- Main Center Stage Login Card -->
  <main class="relative z-10 w-full max-w-md mx-auto px-4 py-6 my-auto">
    <div
      class="w-full p-6 sm:p-8 space-y-6 bg-card border-2 border-border shadow-[5px_5px_0px_0px_var(--neo-shadow)] rounded-lg transition-all"
    >
      <!-- Hero Branding -->
      <div class="text-center space-y-3">
        <div class="w-14 h-14 rounded-xl bg-accent text-accent-text border-2 border-border mx-auto flex items-center justify-center font-black text-2xl shadow-[3px_3px_0px_0px_var(--neo-shadow)] shrink-0">
          T
        </div>
        <div>
          <h2 class="text-xl font-extrabold text-foreground tracking-wide flex items-center justify-center gap-2 font-mono uppercase">
            Termux Monitor
          </h2>
          <p class="text-xs text-muted font-mono mt-1 truncate max-w-[280px] sm:max-w-none mx-auto">
            {sysinfoStore.stats?.model
              ? `${sysinfoStore.stats.model} • Android ${sysinfoStore.stats.android_ver || 'OS'}`
              : 'Android System Control Panel'}
          </p>
        </div>
      </div>

      <!-- Login Form -->
      <form onsubmit={handleSubmit} class="space-y-4 font-mono">
        <div class="space-y-1.5">
          <label for="pwd" class="block text-xs font-bold uppercase tracking-wider text-muted">
            Access Password
          </label>
          <div class="relative">
            <input
              id="pwd"
              type={showPassword ? 'text' : 'password'}
              bind:value={password}
              placeholder="Enter access password"
              required
              class="w-full bg-card-sub border-2 border-border pl-4 pr-11 py-3 text-sm text-foreground focus:outline-none focus:border-accent rounded font-mono transition placeholder:text-muted"
            />
            <button
              type="button"
              onclick={() => (showPassword = !showPassword)}
              class="absolute right-3 top-1/2 -translate-y-1/2 text-muted hover:text-foreground transition p-1 cursor-pointer select-none"
              title="Toggle password visibility"
              aria-label="Toggle password visibility"
            >
              {#if showPassword}
                <EyeOff class="w-4 h-4" />
              {:else}
                <Eye class="w-4 h-4" />
              {/if}
            </button>
          </div>
        </div>

        <!-- Quick Access Auto-Fill Row -->
        <div class="flex items-center justify-between text-xs pt-0.5">
          <span class="text-muted">Quick Access:</span>
          {#if authStore.isDefaultPass}
            <button
              type="button"
              onclick={autofillDefault}
              class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-accent/10 text-accent hover:bg-accent/20 border border-accent/30 transition active:scale-95 cursor-pointer select-none flex items-center gap-1"
            >
              <span>Default: <strong>tmon</strong></span>
              <span class="opacity-75">(Tap to fill)</span>
            </button>
          {:else}
            <span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/30 flex items-center gap-1 select-none">
              <ShieldCheck class="w-3 h-3" />
              <span>Custom Password Active</span>
            </span>
          {/if}
        </div>

        <!-- Animated Error Alert Banner -->
        {#if authStore.loginError}
          <div class="p-3 rounded-xl bg-red-500/10 border border-red-500/30 text-red-400 text-xs flex items-center gap-2">
            <span class="font-bold">⚠️</span>
            <span>{authStore.loginError}</span>
          </div>
        {/if}

        <!-- Submit Button -->
        <!-- Tactile 3D Submit Button -->
        <button
          type="submit"
          disabled={authStore.isLoading || !password}
          class="w-full bg-accent text-accent-text font-mono font-black py-3 text-sm border-2 border-border shadow-[3px_3px_0px_0px_var(--neo-shadow)] hover:shadow-none hover:translate-x-[2px] hover:translate-y-[2px] active:translate-x-[3px] active:translate-y-[3px] rounded transition-all flex items-center justify-center gap-2 select-none disabled:opacity-50 cursor-pointer uppercase tracking-wider"
        >
          {#if authStore.isLoading}
            <span class="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin"></span>
            <span>Authenticating...</span>
          {:else}
            <span>Sign In</span>
            <ArrowRight class="w-4 h-4" />
          {/if}
        </button>
      </form>

      <!-- Project & Support Links Footer -->
      <div class="space-y-3 pt-2 font-mono">
        <div class="relative py-2">
          <div class="absolute inset-0 flex items-center">
            <div class="w-full border-t border-border/60"></div>
          </div>
          <div class="relative flex justify-center text-[10px] uppercase tracking-wider">
            <span class="bg-card px-3 text-muted font-bold">Project Links</span>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-2">
          <!-- GitHub Button -->
          <a
            href="https://github.com/sainz1407/termux-monitor"
            target="_blank"
            rel="noopener noreferrer"
            class="p-2.5 rounded-xl bg-card-sub border border-border/70 text-muted hover:text-foreground hover:border-accent/40 hover:bg-accent/10 transition flex items-center justify-center gap-1.5 text-xs font-bold select-none cursor-pointer group"
          >
            <svg class="w-4 h-4 text-foreground group-hover:scale-110 transition-transform shrink-0" fill="currentColor" viewBox="0 0 24 24">
              <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
            </svg>
            <span class="truncate">GitHub</span>
          </a>

          <!-- Issues Button -->
          <a
            href="https://github.com/sainz1407/termux-monitor/issues"
            target="_blank"
            rel="noopener noreferrer"
            class="p-2.5 rounded-xl bg-card-sub border border-border/70 text-muted hover:text-foreground hover:border-sky-500/40 hover:bg-sky-500/10 transition flex items-center justify-center gap-1.5 text-xs font-bold select-none cursor-pointer group"
          >
            <svg class="w-4 h-4 text-sky-400 group-hover:scale-110 transition-transform shrink-0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 8v4" />
              <path d="M12 16h.01" />
            </svg>
            <span class="truncate">Issues</span>
          </a>
        </div>
      </div>
    </div>
  </main>

  <!-- Bottom Global Footer -->
  <footer class="relative z-10 w-full py-4 text-center font-mono text-[10px] text-muted border-t border-border/40">
    <span>Termux Monitor &bull; Magisk / KernelSU / APatch Module</span>
  </footer>

  <!-- Appearance Studio Modal -->
  <AppearanceModal
    open={showAppearance}
    onclose={() => (showAppearance = false)}
  />
</div>
