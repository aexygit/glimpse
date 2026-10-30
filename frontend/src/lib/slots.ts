// Paper doll: armor left, weapons center bottom, accessories right.
const left = ['Helmet', 'Shoulder', 'Torso', 'Pants', 'Gloves', 'Boots', 'Cape', 'Belt']
const right = ['Necklace', 'Amulet', 'Earring1', 'Earring2', 'Ring1', 'Ring2', 'Bracelet1', 'Bracelet2']

export const layout: { slot: string; row: number; col: number }[] = [
  ...left.map((slot, row) => ({ slot, row, col: 0 })),
  { slot: 'MainHand', row: 6, col: 1 },
  { slot: 'SubHand', row: 7, col: 1 },
  ...right.map((slot, row) => ({ slot, row, col: 2 })),
]

export const label = (slot: string) =>
  slot.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/(\D)(\d)$/, '$1 $2').replace(/ ([A-Z])/g, (_, c) => ' ' + c.toLowerCase())
