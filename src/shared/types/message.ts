/**
 * Message content types for multi-modal chat messages
 */

export interface TextContentItem {
  type: 'text'
  text: string
}

export interface ImageContentItem {
  type: 'image'
  data: string // base64 encoded image data
  mimeType: string // image/png, image/jpeg, image/gif, image/webp
}

export type MessageContentItem = TextContentItem | ImageContentItem
export type MessageContent = MessageContentItem[]
