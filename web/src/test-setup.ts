import "@testing-library/jest-dom/vitest"

// jsdom lacks these browser APIs used by UI libraries.
window.matchMedia ??= ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addEventListener() {},
  removeEventListener() {},
  addListener() {},
  removeListener() {},
  dispatchEvent: () => false,
})) as typeof window.matchMedia
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Element.prototype.scrollIntoView ??= () => {}

// ProseMirror measures text for scrolling; jsdom has no layout.
const rects = () => Object.assign([], { item: () => null }) as unknown as DOMRectList
Range.prototype.getClientRects ??= rects
Range.prototype.getBoundingClientRect ??= () => new DOMRect()
;(Text.prototype as unknown as { getClientRects: () => DOMRectList }).getClientRects ??= rects
document.elementFromPoint ??= () => null
