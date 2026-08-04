package response

import "myobj/src/pkg/custom_type"

type CinemaDirectory struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	ParentID int    `json:"parent_id"`
	Path     string `json:"path"`
}

type CinemaVideoItem struct {
	FileID       string               `json:"file_id"`
	FileName     string               `json:"file_name"`
	FileSize     int                  `json:"file_size"`
	MimeType     string               `json:"mime_type"`
	IsEnc        bool                 `json:"is_enc"`
	HasThumbnail bool                 `json:"has_thumbnail"`
	CreatedAt    custom_type.JsonTime `json:"created_at"`
	Directory    CinemaDirectory      `json:"directory"`
	Tags         []CompactTagView     `json:"tags,omitempty"`
}

type CinemaSection struct {
	Directory CinemaDirectory   `json:"directory"`
	Videos    []CinemaVideoItem `json:"videos"`
	Total     int64             `json:"total"`
	HasMore   bool              `json:"has_more"`
}

type CinemaHomeResponse struct {
	Root     CinemaDirectory `json:"root"`
	Sections []CinemaSection `json:"sections"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	HasMore  bool            `json:"has_more"`
}

type CinemaVideoListResponse struct {
	Root      CinemaDirectory   `json:"root"`
	Directory CinemaDirectory   `json:"directory"`
	Videos    []CinemaVideoItem `json:"videos"`
	Total     int64             `json:"total"`
	Page      int               `json:"page"`
	PageSize  int               `json:"page_size"`
	HasMore   bool              `json:"has_more"`
}

type CinemaVideoDetailResponse struct {
	Root  CinemaDirectory `json:"root"`
	Video CinemaVideoItem `json:"video"`
}

type CinemaRelatedResponse struct {
	Videos   []CinemaVideoItem `json:"videos"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	HasMore  bool              `json:"has_more"`
}
