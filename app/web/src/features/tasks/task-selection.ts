// Group selection changes only members of that group, preserving other selections.
export function updateTaskGroupSelection(
  previous: string[],
  groupIds: ReadonlySet<string>,
  checked: boolean,
  allSelected: boolean,
): string[] {
  const add = allSelected ? !checked : checked
  return add ? [...new Set([...previous, ...groupIds])] : previous.filter((id) => !groupIds.has(id))
}
