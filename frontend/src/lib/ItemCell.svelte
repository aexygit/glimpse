<script lang="ts">
  import { label } from './slots'
  import type { Item } from '../../bindings/github.com/aexygit/glimpse/internal/gear/models'

  let { slot, item, icon, letter, side, flip }: {
    slot: string
    item?: Item
    icon?: string
    letter?: string
    side: 'left' | 'right'
    flip?: boolean
  } = $props()

  const fmt = (v: number) => (Number.isInteger(v) ? String(v) : v.toFixed(1))
  const entries = (s?: { [k: string]: number | undefined } | null) => Object.entries(s ?? {}) as [string, number][]
  let dismissed = $state(false)
  const tipId = $derived(`tip-${slot}`)
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (focusable so the tooltip shows on keyboard focus) -->
<div class={['cell', { empty: !item }]} role="group" tabindex="0" aria-describedby={item ? tipId : undefined}
  aria-label={item ? `${label(slot)}: ${item.Name}` : `${label(slot)}: empty`}
  onkeydown={(e) => e.key === 'Escape' && (dismissed = true)} onblur={() => (dismissed = false)}
  onmouseleave={() => (dismissed = false)}>
  {#if icon}<img src={icon} alt="" />{:else}<span class="ph">{slot.slice(0, 3)}</span>{/if}
  {#if item && item.Enchant > 0}<span class="ench">+{item.Enchant}</span>{/if}
  {#if letter}<span class="badge g{letter}">{letter}</span>{/if}

  {#if item}
    <div class={[`tip ${side}`, { flip, dismissed }]} id={tipId} role="tooltip">
      <strong>{item.Name || `Unknown item #${item.ID}`}</strong>
      <div class="muted">{label(slot)}{item.Tier ? `, tier ${item.Tier}` : ''}{item.Enchant ? `, +${item.Enchant}` : ''}</div>
      {#each entries(item.Stats) as [k, v] (k)}<div class="row"><span>{k}</span><span>{fmt(v)}</span></div>{/each}
    </div>
  {/if}
</div>

<style>
  .cell { position: relative; width: 100%; height: 100%; background: var(--surface-2); border: 1px solid var(--border); border-radius: 6px; display: flex; align-items: center; justify-content: center; }
  .cell.empty { opacity: 0.6; border-style: dashed; }
  .cell:hover, .cell:focus { border-color: var(--accent); outline: none; }
  .cell:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  img { width: 80%; height: 80%; object-fit: contain; }
  .ph { color: var(--muted); font-size: 0.7rem; }
  .ench { position: absolute; left: 3px; bottom: 2px; font-size: 0.7rem; font-weight: 700; color: var(--accent); text-shadow: 0 0 3px #000; }
  .badge { position: absolute; right: 2px; top: 2px; min-width: 1.2em; text-align: center; font-size: 0.7rem; font-weight: 700; border-radius: 3px; padding: 0 3px; background: #000c; color: var(--text); border: 1px solid currentColor; }
  .gS { color: #ffd34d } .gA { color: #6fd36f } .gB { color: #5fb3ff } .gC { color: #e0a050 } .gD { color: #ff7070 }
  .tip { display: none; position: absolute; top: 0; max-height: 300px; overflow-y: auto; z-index: 10; min-width: 180px; padding: 8px 10px; background: var(--bg); border: 1px solid var(--accent); border-radius: 6px; font-size: 0.8rem; pointer-events: none; }
  .tip.flip { top: auto; bottom: 0; }
  .tip.left { left: calc(100% + 6px); }
  .tip.right { right: calc(100% + 6px); }
  .cell:hover .tip, .cell:focus .tip { display: block; }
  .tip.dismissed { display: none !important; }
  .row { display: flex; justify-content: space-between; gap: 12px; }
  .muted { color: var(--muted); }
</style>
