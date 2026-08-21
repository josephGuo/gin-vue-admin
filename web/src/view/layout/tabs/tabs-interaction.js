const SCROLL_EDGE_TOLERANCE = 1

export const reorderTabs = (tabs, oldIndex, newIndex) => {
  const reordered = [...tabs]
  const lastIndex = reordered.length - 1

  if (
    !Number.isInteger(oldIndex) ||
    !Number.isInteger(newIndex) ||
    oldIndex < 0 ||
    newIndex < 0 ||
    oldIndex > lastIndex ||
    newIndex > lastIndex ||
    oldIndex === newIndex
  ) {
    return reordered
  }

  const [movedTab] = reordered.splice(oldIndex, 1)
  reordered.splice(newIndex, 0, movedTab)
  return reordered
}

export const getHorizontalScrollState = ({
  scrollLeft,
  clientWidth,
  scrollWidth
}) => {
  const maxScrollLeft = Math.max(0, scrollWidth - clientWidth)
  const hasOverflow = maxScrollLeft > SCROLL_EDGE_TOLERANCE

  return {
    hasOverflow,
    canScrollLeft: hasOverflow && scrollLeft > SCROLL_EDGE_TOLERANCE,
    canScrollRight:
      hasOverflow &&
      scrollLeft < maxScrollLeft - SCROLL_EDGE_TOLERANCE
  }
}
