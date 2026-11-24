/** The icon set of the interface canvas, shape by shape (24 × 24, stroked with currentColor). */
export interface IconShape {
  tag: 'path' | 'circle' | 'rect'
  attrs: Record<string, string>
}

const path = (d: string): IconShape => ({ tag: 'path', attrs: { d } })
const circle = (cx: string, cy: string, r: string): IconShape => ({
  tag: 'circle',
  attrs: { cx, cy, r },
})
const rect = (x: string, y: string, width: string, height: string, rx: string): IconShape => ({
  tag: 'rect',
  attrs: { x, y, width, height, rx },
})

export const CANVAS_ICONS = {
  home: [path('M4 11 12 4l8 7v9h-5v-6H9v6H4v-9Z')],
  folder: [path('M3 7h7l2 2h9v10H3V7Z')],
  inbox: [path('M4 13h4l2 3h4l2-3h4'), path('M5 5h14l1 8v6H4v-6l1-8Z')],
  agent: [rect('5', '7', '14', '11', '3'), path('M12 3v4M9 12h.01M15 12h.01M9 15.5h6')],
  settings: [
    circle('12', '12', '3'),
    path('M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9 7 7M17 17l2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1'),
  ],
  search: [circle('11', '11', '7'), path('m20 20-4-4')],
  bell: [path('M6 16V11a6 6 0 1 1 12 0v5l1.5 2h-15L6 16Z'), path('M10 20a2 2 0 0 0 4 0')],
  chevronDown: [path('m6 9 6 6 6-6')],
  chevronRight: [path('m8 5 8 7-8 7')],
  monitor: [rect('3', '4', '18', '12', '2'), path('M8 20h8M12 16v4')],
  globe: [circle('12', '12', '9'), path('M3 12h18M12 3c3 3.5 3 14.5 0 18M12 3c-3 3.5-3 14.5 0 18')],
  signOut: [path('M15 4h4v16h-4M10 8l-4 4 4 4M6 12h10')],
  bolt: [path('M13 2 4 14h7l-1 8 9-12h-7l1-8Z')],
  alert: [path('M12 3 2 20h20L12 3Z'), path('M12 10v4M12 17h.01')],
  plus: [path('M12 5v14M5 12h14')],
  check: [path('M5 12l5 5 9-10')],
  checkSmall: [path('M5 12l4 4 10-10')],
  pause: [path('M8 5v14M16 5v14')],
  gate: [path('M4 20V8l8-4 8 4v12'), path('M9 20v-6h6v6')],
  play: [path('M7 4v16l13-8L7 4Z')],
  question: [
    circle('12', '12', '9'),
    path('M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6M12 17h.01'),
  ],
  document: [path('M6 3h8l4 4v14H6V3Z'), path('M14 3v4h4M9 12h6M9 16h6')],
  pin: [path('M12 21s-7-6.2-7-11a7 7 0 1 1 14 0c0 4.8-7 11-7 11Z'), circle('12', '10', '2.5')],
  close: [path('M6 6l12 12M18 6 6 18')],
  copy: [
    rect('8', '8', '12', '12', '2'),
    path('M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3'),
  ],
} satisfies Record<string, IconShape[]>

export type CanvasIconName = keyof typeof CANVAS_ICONS
