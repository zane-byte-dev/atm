import { imageUploadError } from './image-uploads'
export const draftImagePrefix = 'atm-draft-image:'
function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('atm-task-images', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('images')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}
async function access<T>(
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await database()
  try {
    return await new Promise<T>((resolve, reject) => {
      const transaction = db.transaction('images', mode)
      const request = action(transaction.objectStore('images'))
      transaction.oncomplete = () => resolve(request.result)
      transaction.onabort = () => reject(transaction.error || request.error)
      transaction.onerror = () => reject(transaction.error || request.error)
    })
  } finally {
    db.close()
  }
}
export async function storeDraftImage(file: File) {
  const error = imageUploadError(file)
  if (error) throw new Error(error)
  const id = crypto.randomUUID()
  await access('readwrite', (store) => store.put(file, id))
  return { name: file.name || '图片', url: draftImagePrefix + id }
}
export async function readDraftImage(url: string): Promise<File> {
  const file = await access('readonly', (store) => store.get(url.slice(draftImagePrefix.length)))
  if (!(file instanceof Blob)) throw new Error('草稿图片已丢失，请重新插入图片。')
  return file as File
}
export function draftImageURLs(text: string): string[] {
  return [...new Set(text.match(/atm-draft-image:[a-f0-9-]+/g) || [])]
}
export function removeDraftImageMarkup(text: string) {
  return text.replace(/!\[[^\]]*\]\(<?atm-draft-image:[a-f0-9-]+>?\)/g, '')
}
export async function removeDraftImages(urls: string[]) {
  await Promise.all(
    urls.map((url) =>
      access('readwrite', (store) => store.delete(url.slice(draftImagePrefix.length))),
    ),
  )
}
