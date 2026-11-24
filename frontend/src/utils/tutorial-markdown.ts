import { marked } from 'marked'
import DOMPurify from 'dompurify'

export interface TutorialHeading {
  id: string
  text: string
  level: number
}
export function renderTutorial(
  markdown: string,
  copyLabel: string,
  articleTitle?: string,
) {
  const source = markdown.replace(
    /tutorial-asset:([a-f0-9]{32})\b/g,
    '/__tutorial_asset/$1',
  )
  const safe = DOMPurify.sanitize(
    marked.parse(source, { async: false, gfm: true }),
    {
      FORBID_TAGS: [
        'style',
        'iframe',
        'form',
        'input',
        'textarea',
        'select',
        'button',
        'video',
        'audio',
        'object',
        'embed',
        'svg',
        'math',
      ],
      FORBID_ATTR: ['style', 'srcset', 'id', 'name'],
      ALLOW_DATA_ATTR: false,
    },
  )
  const doc = new DOMParser().parseFromString(safe, 'text/html')
  const headings: TutorialHeading[] = []
  let titleId: string | undefined
  doc.querySelectorAll('h1,h2,h3,h4').forEach((h, index) => {
    h.id = `tutorial-section-${index}`
    if (
      h.tagName === 'H1' &&
      h === doc.body.firstElementChild &&
      articleTitle?.trim() === h.textContent?.trim() &&
      !h.querySelector('a,img')
    ) {
      titleId = h.id
      h.remove()
      return
    }
    headings.push({
      id: h.id,
      text: h.textContent || '',
      level: Number(h.tagName.slice(1)),
    })
  })
  doc.querySelectorAll('img').forEach((img) => {
    const asset = /^\/__tutorial_asset\/([a-f0-9]{32})$/.exec(
      img.getAttribute('src') || '',
    )?.[1]
    img.removeAttribute('src')
    if (asset) {
      img.dataset.asset = asset
      img.tabIndex = 0
      img.setAttribute('role', 'button')
    } else img.remove()
  })
  doc.querySelectorAll('a').forEach((a) => {
    const href = a.getAttribute('href') || ''
    // Only web links and local navigation, never protocol-relative or custom schemes.
    if (!/^(https?:\/\/|\/(?!\/)|#)/i.test(href)) a.removeAttribute('href')
    if (/^https?:/i.test(href)) {
      a.target = '_blank'
      a.rel = 'noopener noreferrer'
    }
  })
  doc.querySelectorAll('pre').forEach((pre) => {
    const button = doc.createElement('button')
    button.type = 'button'
    button.dataset.copy = 'true'
    button.textContent = copyLabel
    button.className = 'tutorial-copy'
    pre.append(button)
  })
  return { html: doc.body.innerHTML, headings, titleId }
}
