<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { DownloadAssets, Paths } from '../../bindings/github.com/aexygit/glimpse/internal/app/service'

  let { onclose }: { onclose: () => void } = $props()
  let paths = $state<Record<string, string | undefined>>({})
  let progress = $state({ done: 0, total: 0 })
  let busy = $state(false)
  let error = $state('')

  onMount(() => {
    Paths().then((p) => (paths = p)).catch((e) => (error = String(e)))
    return Events.On('assets:progress', (e) => (progress = e.data))
  })

  async function download() {
    busy = true
    error = ''
    try {
      await DownloadAssets()
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }
</script>

<section aria-labelledby="settings-h">
  <div class="head"><h2 id="settings-h">Settings</h2><button onclick={onclose}>Close</button></div>
  <button onclick={download} disabled={busy}>{busy ? 'Downloading' : 'Download all assets'}</button>
  {#if progress.total > 0}
    <progress max={progress.total} value={progress.done} aria-label="Asset download"></progress>
    <span>{progress.done} / {progress.total}</span>
  {/if}
  {#if error}<p class="err" role="alert">{error}</p>{/if}
  <dl>
    {#each Object.entries(paths) as [k, v]}<dt>{k}</dt><dd>{v}</dd>{/each}
  </dl>
</section>

<style>
  section { padding: 12px; }
  .head { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px; font-size: 1.1rem; }
  button { background: var(--surface-2); color: var(--text); border: 1px solid var(--border); border-radius: 4px; padding: 4px 10px; cursor: pointer; }
  button:hover:not(:disabled) { border-color: var(--accent); }
  progress { width: 240px; vertical-align: middle; margin: 0 8px; }
  dt { color: var(--muted); margin-top: 8px; text-transform: capitalize; }
  dd { margin: 0; word-break: break-all; }
  .err { color: var(--bad); }
</style>
