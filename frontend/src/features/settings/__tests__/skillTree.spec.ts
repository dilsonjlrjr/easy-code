import { describe, expect, it } from 'vitest'
import { buildSkillTree, countFiles } from '../skillTree'

describe('buildSkillTree', () => {
  it('nests the files of a package under their folders, SKILL.md first', () => {
    const tree = buildSkillTree([
      { path: 'references/testes.md', estimated_tokens: 200 },
      { path: 'SKILL.md', estimated_tokens: 1000 },
      { path: 'references/api.md', estimated_tokens: 300 },
      { path: 'LICENSE', estimated_tokens: 10 },
    ])

    expect(tree.map((node) => [node.name, node.kind])).toEqual([
      ['SKILL.md', 'file'],
      ['LICENSE', 'file'],
      ['references', 'folder'],
    ])
    const references = tree[2]
    expect(references?.path).toBe('references')
    expect(references?.estimatedTokens).toBe(500)
    expect(references?.children.map((node) => node.path)).toEqual([
      'references/api.md',
      'references/testes.md',
    ])
    expect(countFiles(tree)).toBe(4)
  })

  it('gives an empty tree when the API sends no files', () => {
    expect(buildSkillTree(undefined)).toEqual([])
    expect(countFiles([])).toBe(0)
  })
})
