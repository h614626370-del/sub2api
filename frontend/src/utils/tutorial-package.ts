import type { TutorialPackageError } from '@/api/tutorials'
import type { ComposerTranslation } from 'vue-i18n'

const errorKeys: Record<string, string> = {
  ZIP_SIZE: 'zipSize',
  ZIP_INVALID: 'zipInvalid',
  ZIP_REQUIRED: 'zipRequired',
  PATH_INVALID: 'pathInvalid',
  LAYOUT: 'layout',
  MARKDOWN_SIZE: 'markdownSize',
  METADATA: 'metadata',
  CONTENT_INVALID: 'content',
  IMAGE_FORMAT: 'imageFormat',
  IMAGE_SIZE: 'imageSize',
  IMAGE_QUOTA: 'imageQuota',
  IMAGE_MISSING: 'imageMissing',
  IMAGE_PATH: 'imagePath',
}

export function tutorialPackageError(t: ComposerTranslation, error: unknown, fallback: string) {
  const e = error as TutorialPackageError | null
  if (e?.status === 413) return t('tutorials.packageErrors.zipSize')
  const code = e?.reason?.startsWith('TUTORIAL_PACKAGE_')
    ? e.reason.slice('TUTORIAL_PACKAGE_'.length)
    : ''
  const key = Object.prototype.hasOwnProperty.call(errorKeys, code) ? errorKeys[code] : undefined
  const message = key ? t(`tutorials.packageErrors.${key}`) : e?.message || fallback
  return e?.metadata?.file ? `${message} (${e.metadata.file})` : message
}
