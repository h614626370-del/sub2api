import { describe, expect, it } from 'vitest'
import { renderTutorial } from '../tutorial-markdown'

describe('tutorial Markdown', () => {
  it('renders headings, tables and code with a stable table of contents', () => {
    const result = renderTutorial(
      '## 开始\n\n### 配置\n\n```bash\necho OK\n```\n\n| A |\n| --- |\n| B |',
      '复制代码',
    )
    expect(result.headings.map((h) => h.text)).toEqual(['开始', '配置'])
    expect(result.html).toContain('id="tutorial-section-0"')
    expect(result.html).toContain('data-copy="true"')
    expect(result.html).toContain('<table>')
  })
  it('shows a matching lead title once and preserves existing section anchors', () => {
    const markdown = '# **Getting started**\n\n## Config\n\n# Getting started'
    const result = renderTutorial(markdown, 'Copy', 'Getting started')
    expect(result.titleId).toBe('tutorial-section-0')
    expect(result.headings.map((heading) => heading.id)).toEqual([
      'tutorial-section-1',
      'tutorial-section-2',
    ])
    expect(result.html).not.toContain('tutorial-section-0')
    expect(renderTutorial(markdown, 'Copy').headings).toHaveLength(3)
  })
  it('leaves uploaded images inert until authenticated blob loading', () => {
    const key = '0123456789abcdef0123456789abcdef'
    const result = renderTutorial(
      `![截图](tutorial-asset:${key})\n\n![remote](https://tracker.invalid/pixel)`,
      'Copy',
    )
    const doc = new DOMParser().parseFromString(result.html, 'text/html')
    expect(doc.querySelectorAll('img')).toHaveLength(1)
    expect(doc.querySelector('img')?.dataset.asset).toBe(key)
    expect(doc.querySelector('img')?.hasAttribute('src')).toBe(false)
  })
  it('removes executable content, event handlers, styles and unsafe links', () => {
    const result = renderTutorial(
      '<script>alert(1)</script><iframe src="/evil"></iframe><svg onload="alert(1)"></svg><form><input /></form>\n\n<a href="javascript:alert(1)" onclick="alert(1)" style="position:fixed">bad</a>\n\n[external](https://example.com)',
      'Copy',
    )
    const doc = new DOMParser().parseFromString(result.html, 'text/html')
    expect(
      doc.querySelector('script,iframe,svg,form,input,[onclick],[style]'),
    ).toBeNull()
    expect(doc.querySelector('a')?.hasAttribute('href')).toBe(false)
    expect(doc.querySelector('a[href="https://example.com"]')?.rel).toBe(
      'noopener noreferrer',
    )
  })
})
