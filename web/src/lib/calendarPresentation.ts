import type { BlockInput } from './layout'

/** Visual insets never change scheduling, overlap detection or capacity. */
export function blockGeometry(block: BlockInput & { lane: number; lanes: number }, pxPerMinute: number, minHeight: number) {
  const step = block.lanes > 1 ? 100 / (block.lanes + 1) : 100
  const left = block.lane * step
  const width = Math.min(100 - left, block.lanes > 1 ? step * 3 : 100)
  return {
    top: block.start * pxPerMinute + 2,
    // At least 24 px, the WCAG 2.5.8 target size: the gap gives way on the shortest blocks.
    height: Math.max(24, Math.max(minHeight, (block.end - block.start) * pxPerMinute) - 4),
    left: `calc(${left}% + 3px)`, width: `calc(${width}% - 6px)`, zIndex: 10 + block.lane,
  }
}

export function isWeekend(date: string): boolean {
  const day = new Date(`${date}T12:00:00`).getDay()
  return day === 0 || day === 6
}
