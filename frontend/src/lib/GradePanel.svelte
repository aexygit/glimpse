<script lang="ts">
  import type { Inspected } from '../../bindings/github.com/aexygit/glimpse/internal/app/models'

  import { label } from './slots'
  let { inspected }: { inspected: Inspected } = $props()

  const reason = (r: string, target: number) =>
    ({ missing: 'No item equipped', tier: 'Upgrade to a higher tier', enchant: `Enchant to +${target}`, rolls: 'Better stat rolls' })[r] ?? r
  const top = $derived(inspected.Report.Upgrades.slice(0, 3))
</script>

<aside>
  <h2>{inspected.Character.Name}</h2>
  <p class="muted">{inspected.Character.Class}, level {inspected.Character.Level}</p>
  {#if inspected.Graded}
    <div class="overall"><span class="letter">{inspected.Report.Letter}</span>
      <span>{Math.round(inspected.Report.Overall * 100)}%</span></div>
    <h3>Top upgrades</h3>
    {#if top.length}
      <ol>
        {#each top as u}
          <li><strong>{label(u.Slot)}</strong><br /><span class="muted">{reason(u.Reason, u.Target)}</span></li>
        {/each}
      </ol>
    {:else}
      <p class="muted">No upgrades suggested</p>
    {/if}
  {:else}
    <div class="overall"><span class="letter">?</span></div>
    <p>No grading rules for this class yet</p>
  {/if}
</aside>

<style>
  aside { min-width: 220px; }
  h2 { margin: 0; font-size: 1.2rem; }
  h3 { font-size: 0.9rem; margin: 16px 0 6px; }
  .overall { display: flex; align-items: baseline; gap: 12px; font-size: 1.4rem; margin-top: 12px; }
  .letter { font-size: 3rem; font-weight: 800; color: var(--accent); }
  ol { margin: 0; padding-left: 20px; }
  li { margin-bottom: 8px; }
  .muted { color: var(--muted); }
</style>
