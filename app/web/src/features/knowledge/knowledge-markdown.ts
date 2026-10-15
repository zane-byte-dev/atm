import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
const markdown = unified().use(remarkParse).use(remarkGfm)
type Node = { type: string; children?: Node[]; [key: string]: unknown }
// Compare meaning, ignoring serializer choices such as bullet markers and spacing.
export function markdownMeaning(source: string): string {
  const root = markdown.parse(source) as Node
  const definitions = new Map<string, Node>()
  const collect = (node: Node) => {
    if (node.type === 'definition') definitions.set(String(node.identifier), node)
    node.children?.forEach(collect)
  }
  collect(root)
  const normalize = (node: Node): unknown => {
    if (node.type === 'definition') return undefined
    let value = { ...node }
    if (node.type === 'linkReference' || node.type === 'imageReference') {
      const definition = definitions.get(String(node.identifier))
      if (definition) {
        value = {
          ...value,
          type: node.type === 'linkReference' ? 'link' : 'image',
          url: definition.url,
          title: definition.title,
        }
        delete value.identifier
        delete value.label
        delete value.referenceType
      }
    }
    return Object.fromEntries(
      Object.entries(value)
        .filter(([key, item]) => key !== 'position' && key !== 'spread' && item != null)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, item]) => [
          key,
          key === 'children'
            ? (item as Node[]).map(normalize).filter((child) => child !== undefined)
            : item,
        ]),
    )
  }
  return JSON.stringify(normalize(root))
}
export function canRoundTripMarkdown(source: string, serialized: string): boolean {
  return markdownMeaning(source) === markdownMeaning(serialized)
}
