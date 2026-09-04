export const taskImageTypes = ['image/png', 'image/jpeg', 'image/gif']
export const taskImageLimit = 10
export const taskImageMaxBytes = 10 * 1024 * 1024
export function imageUploadError(file: Pick<File, 'type' | 'size' | 'name'>): string | undefined {
  if (!taskImageTypes.includes(file.type))
    return `${file.name || '这张图片'}：仅支持 PNG、JPEG、GIF。`
  if (file.size <= 0) return `${file.name || '这张图片'}：图片为空。`
  if (file.size > taskImageMaxBytes) return `${file.name || '这张图片'}：图片不能超过 10 MB。`
}
