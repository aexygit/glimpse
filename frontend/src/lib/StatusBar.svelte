<script lang="ts">
  import { Status } from '../../bindings/github.com/aexygit/glimpse/internal/capture/models'
  import type { Inspected } from '../../bindings/github.com/aexygit/glimpse/internal/app/models'

  let { status, session, selectedID, onselect, onsettings }: {
    status: Status
    session: Inspected[]
    selectedID: number | null
    onselect: (id: number) => void
    onsettings: () => void
  } = $props()

  const text: Record<string, string> = {
    needs_admin: 'Run Glimpse as Administrator',
    waiting_game: 'Waiting for Aion 2',
    capturing: 'Capturing',
    error: 'Capture error, retrying',
  }
</script>

<header>
  <span class="status {status}" role="status">{text[status] ?? 'Starting'}</span>
  <nav aria-label="Inspected characters">
    {#each [...session].reverse() as s (s.ID)}
      <button class:active={s.ID === selectedID} aria-pressed={s.ID === selectedID} onclick={() => onselect(s.ID)}>
        {s.Character.Name} <small>{s.Report.Letter}</small>
      </button>
    {/each}
  </nav>
  <button class="settings" onclick={onsettings}>Settings</button>
</header>

<style>
  header { display: flex; align-items: center; gap: 12px; padding: 8px 12px; background: var(--surface); border-bottom: 1px solid var(--border); }
  .status { font-weight: 600; white-space: nowrap; padding: 2px 10px; border-radius: 4px; border: 1px solid var(--border); }
  .status.capturing { color: var(--good); border-color: var(--good); }
  .status.needs_admin, .status.error { color: var(--bad); border-color: var(--bad); }
  nav { display: flex; gap: 6px; flex: 1; overflow-x: auto; }
  button { background: var(--surface-2); color: var(--text); border: 1px solid var(--border); border-radius: 4px; padding: 4px 10px; cursor: pointer; white-space: nowrap; }
  button:hover { border-color: var(--accent); }
  button.active { border-color: var(--accent); background: var(--accent-bg); }
  small { color: var(--muted); }
</style>
