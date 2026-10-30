<script lang="ts">
  import type { Inspected } from '../../bindings/github.com/aexygit/glimpse/internal/app/models'
  import ItemCell from './ItemCell.svelte'
  import { layout } from './slots'

  let { inspected }: { inspected: Inspected } = $props()
  const letters = $derived(Object.fromEntries(inspected.Report.Slots.map((s) => [s.Slot, s.Letter])))
</script>

<div class="grid" role="group" aria-label="Equipment of {inspected.Character.Name}">
  {#each layout as { slot, row, col } (slot)}
    <div style="grid-row: {row + 1}; grid-column: {col + 1}">
      <ItemCell {slot} item={inspected.Character.Items[slot]} icon={inspected.Icons[slot]}
        letter={letters[slot]} side={col === 2 ? 'right' : 'left'} flip={row >= 4} />
    </div>
  {/each}
</div>

<style>
  .grid { display: grid; grid-template-columns: repeat(3, 54px); grid-auto-rows: 54px; gap: 6px; justify-content: center; }
</style>
