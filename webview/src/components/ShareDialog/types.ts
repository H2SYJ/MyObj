export interface ShareDialogFileInfo {
  file_id: string
  file_name: string
  file_size?: number
}

export interface ShareExpireOption {
  label: string
  value: number
}

export interface ShareDialogResult {
  shareUrl: string
  expireText: string
  copied: boolean
  passwordCopied: boolean
}
