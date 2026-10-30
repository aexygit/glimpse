<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { Session, Status as GetStatus, Warnings } from '../bindings/github.com/aexygit/glimpse/internal/app/service'
  import type { Inspected } from '../bindings/github.com/aexygit/glimpse/internal/app/models'
  import { Status } from '../bindings/github.com/aexygit/glimpse/internal/capture/models'
  import StatusBar from './lib/StatusBar.svelte'
  import GearGrid from './lib/GearGrid.svelte'
  import GradePanel from './lib/GradePanel.svelte'
  import Settings from './lib/Settings.svelte'

  let session = $state<Inspected[]>([])
  let status = $state<Status>(Status.$zero)
  let warnings = $state<string[]>([])
  let selectedID = $state<number | null>(null)
  let showSettings = $state(false)

  // Go nil slices and maps arrive as null in event payloads.
  function normalize(i: Inspected): Inspected {
    i.Character.Items = i.Character.Items ?? {}
    for (const it of Object.values(i.Character.Items)) {
      if (!it) continue
      it.Stats = it.Stats ?? {}
      it.Sockets = (it.Sockets ?? []).map((s) => s ?? {})
    }
    i.Report.Slots = i.Report.Slots ?? []
    i.Report.Upgrades = i.Report.Upgrades ?? []
    i.Icons = i.Icons ?? {}
    return i
  }

  const selected = $derived(session.find((s) => s.ID === selectedID))

  onMount(() => {
    // Subscribe first, then fetch state, so nothing is missed in between.
    const offs = [
      Events.On('inspect', (e) => {
        const i = normalize(e.data)
        if (!session.some((s) => s.ID === i.ID)) session.push(i)
        selectedID = i.ID
      }),
      Events.On('inspect:update', (e) => {
        const i = normalize(e.data)
        session = session.map((s) => (s.ID === i.ID ? i : s))
      }),
      Events.On('status', (e) => (status = e.data)),
      Events.On('rules:warning', (e) => {
        if (!warnings.includes(e.data)) warnings.push(e.data)
      }),
    ]
    loadState()
    return () => offs.forEach((off) => off())
  })

  async function loadState() {
    const [s, st, w] = await Promise.all([Session(), GetStatus(), Warnings()])
    for (const i of s) if (!session.some((x) => x.ID === i.ID)) session.push(normalize(i))
    session.sort((a, b) => a.ID - b.ID)
    if (selectedID === null && s.length) selectedID = session[session.length - 1].ID
    if (status === Status.$zero) status = st
    for (const m of w) if (!warnings.includes(m)) warnings.push(m)
  }
</script>

<StatusBar {status} {session} {selectedID} onselect={(id) => (selectedID = id)} onsettings={() => (showSettings = !showSettings)} />

{#each warnings as w, i (w)}
  <div class="warn" role="alert"><span>{w}</span>
    <button onclick={() => warnings.splice(i, 1)} aria-label="Dismiss warning">Dismiss</button></div>
{/each}

<main>
  {#if showSettings}
    <Settings onclose={() => (showSettings = false)} />
  {:else if selected}
    <GearGrid inspected={selected} />
    <GradePanel inspected={selected} />
  {:else}
    <p class="empty">Inspect a character in game to see their gear here.</p>
  {/if}
</main>

<style>
  :global(:root) {
    --bg: #14161b; --surface: #1c1f26; --surface-2: #252932; --border: #3a3f4b;
    --text: #e8eaee; --muted: #a0a6b3; --accent: #e0b050; --accent-bg: #3a3220;
    --good: #6fd36f; --bad: #ff8080;
  }
  :global(body) { margin: 0; background: var(--bg); color: var(--text); font: 14px/1.4 system-ui, sans-serif; }
  main { display: flex; flex-wrap: wrap; gap: 32px; justify-content: center; padding: 16px; }
  .empty { color: var(--muted); }
  .warn { display: flex; justify-content: space-between; gap: 12px; padding: 6px 12px; background: #4a3a14; border-bottom: 1px solid var(--accent); }
  .warn button { background: none; color: var(--text); border: 1px solid var(--text); border-radius: 4px; cursor: pointer; }
</style>
