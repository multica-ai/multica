"use client"

import { useFileUpload } from "@multica/ui/hooks/use-file-upload"

import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from "@multica/ui/components/ui/avatar"
import { Button } from "@multica/ui/components/ui/button"
import { UserCircle } from "lucide-react"

interface ImageUploadFieldProps {
  inputId?: string
  defaultImage?: string
  alt?: string
  description?: string
  uploadLabel?: string
  replaceLabel?: string
}

export function ImageUploadField({
  inputId,
  defaultImage,
  alt = "Uploaded image",
  description,
  uploadLabel = "Upload photo",
  replaceLabel = "Replace photo",
}: ImageUploadFieldProps) {
  const [{ files }, { removeFile, openFileDialog, getInputProps }] =
    useFileUpload({
      accept: "image/*",
    })

  const currentFile = files[0] ?? null
  const previewUrl = currentFile?.preview ?? defaultImage ?? null
  const fileName = currentFile?.file.name
  const hasImage = Boolean(previewUrl)

  const handleCancelUpload = () => {
    if (!currentFile) {
      return
    }

    removeFile(currentFile.id)
  }

  return (
    <div className="flex items-center gap-3">
      <Avatar className="size-12 border">
        {previewUrl ? (
          <AvatarImage src={previewUrl} alt={fileName ?? alt} />
        ) : null}
        <AvatarFallback className="bg-muted text-muted-foreground">
          <UserCircle aria-hidden="true" className="size-5 opacity-60" />
        </AvatarFallback>
      </Avatar>

      <div className="flex min-w-0 flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative inline-flex">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={openFileDialog}
              aria-haspopup="dialog"
            >
              {hasImage ? replaceLabel : uploadLabel}
            </Button>
            <input
              {...getInputProps({ id: inputId })}
              className="hidden"
              aria-hidden="true"
              tabIndex={-1}
            />
          </div>

          {currentFile ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={handleCancelUpload}
              className="text-muted-foreground hover:text-foreground"
            >
              Cancel
            </Button>
          ) : null}
        </div>

        {description ? (
          <p className="text-muted-foreground text-caption">
            {description}
          </p>
        ) : null}
      </div>
    </div>
  )
}