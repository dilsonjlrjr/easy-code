import type { SkillFile } from '@/features/agents/api/agentsApi'

/** A node of the package tree of a skill: a folder with its children, or a file. */
export interface SkillTreeNode {
  name: string
  path: string
  kind: 'folder' | 'file'
  estimatedTokens: number
  children: SkillTreeNode[]
}

function compareNodes(left: SkillTreeNode, right: SkillTreeNode): number {
  if (left.name === 'SKILL.md' || right.name === 'SKILL.md') {
    return left.name === 'SKILL.md' ? -1 : 1
  }
  if (left.kind !== right.kind) return left.kind === 'file' ? -1 : 1
  return left.name.localeCompare(right.name)
}

function sortTree(nodes: SkillTreeNode[]): SkillTreeNode[] {
  for (const node of nodes) node.children = sortTree(node.children)
  return nodes.sort(compareNodes)
}

/**
 * Builds the tree of a skill package from the flat file list of the API: folders from the path
 * segments, each folder with the sum of the tokens below it. At each level SKILL.md comes first,
 * then the files, then the folders, each group by name.
 */
export function buildSkillTree(files: SkillFile[] | undefined): SkillTreeNode[] {
  const root: SkillTreeNode[] = []
  for (const file of files ?? []) {
    const segments = file.path.split('/').filter((segment) => segment !== '')
    let level = root
    let path = ''
    segments.forEach((segment, index) => {
      path = path ? `${path}/${segment}` : segment
      const isFile = index === segments.length - 1
      let node = level.find(
        (item) => item.name === segment && item.kind === (isFile ? 'file' : 'folder'),
      )
      if (!node) {
        node = {
          name: segment,
          path,
          kind: isFile ? 'file' : 'folder',
          estimatedTokens: 0,
          children: [],
        }
        level.push(node)
      }
      node.estimatedTokens += file.estimated_tokens
      level = node.children
    })
  }
  return sortTree(root)
}

/** Number of files below the nodes (folders do not count). */
export function countFiles(nodes: SkillTreeNode[]): number {
  return nodes.reduce(
    (total, node) => total + (node.kind === 'file' ? 1 : countFiles(node.children)),
    0,
  )
}
